package e2etest

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// User is a simple user bot.
type User struct {
	suite    *Suite
	text     []string
	username string

	logger  *zerolog.Logger
	message chan string
}

// NewUser creates new User bot.
func NewUser(suite *Suite, text []string, username string) User {
	lg := suite.logger.With().Str("logger", "terentyev").Logger()
	return User{
		suite:    suite,
		text:     text,
		username: username,
		logger:   &lg,
		message:  make(chan string, 1),
	}
}

func (u User) messageHandler(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
	if filterMessage(update) {
		return nil
	}

	if m, ok := update.Message.(interface{ GetMessage() string }); ok {
		logger := u.logger.With().
			Str("logger", "dispatcher").
			Str("message", m.GetMessage()).
			Logger()
		logger.Info().Msg("Got new message update")
	}

	msg, ok := update.Message.(*tg.Message)
	if !ok {
		return errors.Errorf("unexpected type %T", update.Message)
	}

	select {
	case u.message <- msg.Message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run setups and starts user bot.
func (u User) Run(ctx context.Context) error {
	dispatcher := tg.NewUpdateDispatcher()
	dispatcher.OnNewMessage(u.messageHandler)
	client := u.suite.Client(u.logger, dispatcher)
	sender := message.NewSender(tg.NewClient(retryInvoker{prev: client}))

	return client.Run(ctx, func(ctx context.Context) error {
		if err := u.suite.RetryAuthenticate(ctx, client.Auth()); err != nil {
			return errors.Wrap(err, "authenticate")
		}

		peer, err := sender.Resolve(u.username).AsInputPeer(ctx)
		if err != nil {
			return errors.Wrapf(err, "resolve bot username %q", u.username)
		}

		for _, line := range u.text {
			time.Sleep(2 * time.Second)

			_, err = sender.To(peer).Text(ctx, line)
			if flood, err := tgerr.FloodWait(ctx, err); err != nil {
				if flood {
					continue
				}
				return err
			}

			select {
			case gotMessage := <-u.message:
				require.Equal(u.suite.TB, line, gotMessage)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	})
}
