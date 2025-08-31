package mtproto

import (
	"context"

	"github.com/go-faster/errors"
	"go.uber.org/multierr"

	"github.com/gotd/td/exchange"
)

// connect establishes connection using configured transport, creating
// new auth key if needed.
func (c *Conn) connect(ctx context.Context) (rErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.dialTimeout)
	defer cancel()

	conn, err := c.dialer(ctx)
	if err != nil {
		return errors.Wrap(err, "dial failed")
	}
	c.conn = conn
	defer func() {
		if rErr != nil {
			multierr.AppendInto(&rErr, conn.Close())
		}
	}()

	session := c.session()
	if session.Key.Zero() {
		c.log.Info().Msg("Generating new auth key")

		start := c.clock.Now()
		if err := c.createAuthKey(ctx); err != nil {
			return errors.Wrap(err, "create auth key")
		}

		c.log.Info().
			Dur("duration", c.clock.Now().Sub(start)).
			Msg("Auth key generated")

		return nil
	}

	c.log.Info().Msg("Key already exists")
	if session.ID == 0 {
		// NB: Telegram can return 404 error if session id is zero.
		//
		// See https://github.com/gotd/td/issues/107.
		c.log.Debug().Msg("Generating new session id")
		if err := c.newSessionID(); err != nil {
			return err
		}
	}

	return nil
}

// createAuthKey generates new authorization key.
func (c *Conn) createAuthKey(ctx context.Context) error {
	// Grab exclusive lock for writing.
	// It prevents message sending during key regeneration if server forgot current auth key.
	c.exchangeLock.Lock()
	defer c.exchangeLock.Unlock()

	l := c.log.Debug().
		Dur("timeout", c.exchangeTimeout)

	if deadline, ok := ctx.Deadline(); ok {
		l = l.Time("context_deadline", deadline)
	}

	l.Msg("Initializing new key exchange")
	lg := c.log.With().Str("logger", "exchange").Logger()
	r, err := exchange.NewExchanger(c.conn, c.dcID).
		WithClock(c.clock).
		WithLogger(&lg).
		WithTimeout(c.exchangeTimeout).
		WithRand(c.rand).
		Client(c.rsaPublicKeys).
		Run(ctx)

	if err != nil {
		return err
	}

	c.sessionMux.Lock()
	c.authKey = r.AuthKey
	c.sessionID = r.SessionID
	c.salt = r.ServerSalt
	c.sessionMux.Unlock()

	return nil
}
