package telegram

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-faster/errors"
	"github.com/gotd/td/exchange"
	"github.com/gotd/td/tdsync"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tgerr"
	"go.uber.org/multierr"
)

func (c *Client) runUntilRestart(ctx context.Context) error {
	g := tdsync.NewCancellableGroup(ctx)
	g.Go(c.conn.Run)

	if !c.noUpdatesMode {
		g.Go(func(ctx context.Context) error {
			self, err := c.Self(ctx)
			if err != nil {
				if !auth.IsUnauthorized(err) {
					c.log.Warn().Err(err).Msg("Got error on self")
				}
				if h := c.onSelfError; h != nil {
					if err := h(ctx, err); err != nil {
						return errors.Wrap(err, "onSelfError")
					}
				}
				return nil
			}

			c.log.Info().Str("username", self.Username).Msg("Got self")
			return nil
		})
	}

	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.restart:
			c.log.Debug().Msg("Restart triggered")
			g.Cancel()
			return nil
		}
	})

	return g.Wait()
}

func (c *Client) isPermanentError(err error) bool {
	// See https://github.com/gotd/td/issues/1458.
	if errors.Is(err, exchange.ErrKeyFingerprintNotFound) {
		return true
	}
	if tgerr.Is(err, "AUTH_KEY_UNREGISTERED", "SESSION_EXPIRED", "AUTH_KEY_DUPLICATED") {
		return true
	}
	if auth.IsUnauthorized(err) {
		return true
	}
	return false
}
func (c *Client) reconnectUntilClosed(ctx context.Context) error {
	b := tdsync.SyncBackoff(backoff.WithContext(c.newConnBackoff(), ctx))
	c.connBackoff.Store(&b)

	return backoff.RetryNotify(func() error {
		if err := c.runUntilRestart(ctx); err != nil {
			if c.isPermanentError(err) {
				return backoff.Permanent(err)
			}
			return err
		}
		return nil
	}, b, func(err error, timeout time.Duration) {
		c.log.Info().
			Err(err).
			Dur("backoff", timeout).
			Msg("Restarting connection")

		c.connMux.Lock()
		c.conn = c.createPrimaryConn(nil)
		c.connMux.Unlock()
	})
}

func (c *Client) onReady() {
	c.log.Debug().Msg("Ready")
	c.ready.Signal()

	if b := c.connBackoff.Load(); b != nil {
		(*b).Reset()
	}
}

func (c *Client) resetReady() {
	c.ready.Reset()
}

// Run starts client session and blocks until connection close.
// The f callback is called on successful session initialization and Run
// will return on f() result.
//
// Context of callback will be canceled if fatal error is detected.
// The ctx is used for background operations like updates handling or pools.
//
// See `examples/bg-run` and `contrib/gb` package for classic approach without
// explicit callback, with Connect and defer close().
func (c *Client) Run(ctx context.Context, f func(ctx context.Context) error) (err error) {
	if c.ctx != nil {
		select {
		case <-c.ctx.Done():
			return errors.Wrap(c.ctx.Err(), "client already closed")
		default:
		}
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	c.log.Info().Msg("Starting")
	defer c.log.Info().Msg("Closed")
	defer c.cancel()
	defer func() {
		c.subConnsMux.Lock()
		defer c.subConnsMux.Unlock()

		for _, conn := range c.subConns {
			if closeErr := conn.Close(); !errors.Is(closeErr, context.Canceled) {
				multierr.AppendInto(&err, closeErr)
			}
		}
	}()

	c.resetReady()
	if err := c.restoreConnection(ctx); err != nil {
		return err
	}

	g := tdsync.NewCancellableGroup(ctx)
	g.Go(c.reconnectUntilClosed)
	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			c.cancel()
			return ctx.Err()
		case <-c.ctx.Done():
			return c.ctx.Err()
		}
	})
	g.Go(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ready.Ready():
			if err := f(ctx); err != nil {
				return errors.Wrap(err, "callback")
			}
			c.log.Debug().Msg("Callback returned, stopping")
			g.Cancel()
			return nil
		}
	})
	if err := g.Wait(); !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}
