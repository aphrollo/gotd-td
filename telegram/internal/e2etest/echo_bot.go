package e2etest

import (
	"context"
	"strconv"
	"sync"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/rs/zerolog"
)

// EchoBot is a simple echo message bot.
type EchoBot struct {
	suite *Suite

	logger *zerolog.Logger
	auth   chan<- *tg.User
}

// NewEchoBot creates new echo bot.
func NewEchoBot(suite *Suite, auth chan<- *tg.User) EchoBot {
	lg := suite.logger.With().Str("logger", "echobot").Logger()
	return EchoBot{
		suite:  suite,
		logger: &lg,
		auth:   auth,
	}
}

type users struct {
	users map[int64]*tg.User
	lock  sync.RWMutex
}

func newUsers() *users {
	return &users{
		users: map[int64]*tg.User{},
	}
}

func (m *users) empty() (r bool) {
	m.lock.RLock()
	r = len(m.users) < 1
	m.lock.RUnlock()
	return
}

func (m *users) add(list ...tg.UserClass) {
	m.lock.Lock()
	defer m.lock.Unlock()

	tg.UserClassArray(list).FillNotEmptyMap(m.users)
}

func (m *users) get(id int64) (r *tg.User) {
	m.lock.RLock()
	r = m.users[id]
	m.lock.RUnlock()

	return
}

func (b EchoBot) login(ctx context.Context, client *telegram.Client) (*tg.User, error) {
	if err := b.suite.RetryAuthenticate(ctx, client.Auth()); err != nil {
		return nil, errors.Wrap(err, "authenticate")
	}

	var me *tg.User
	if err := retry(ctx, func() (err error) {
		me, err = client.Self(ctx)
		return err
	}); err != nil {
		return nil, err
	}

	expectedUsername := "echobot" + strconv.FormatInt(me.ID, 10)
	raw := tg.NewClient(retryInvoker{prev: client})
	_, err := raw.AccountUpdateUsername(ctx, expectedUsername)
	if err != nil {
		if !tgerr.Is(err, tg.ErrUsernameNotModified) {
			return nil, errors.Wrap(err, "update username")
		}
	}
	me, err = retryResult(ctx, func() (*tg.User, error) {
		return client.Self(ctx)
	})
	if me.Username != expectedUsername {
		return nil, errors.Errorf("expected username %q, got %q", expectedUsername, me.Username)
	}

	return me, nil
}

func (b EchoBot) handler(client *telegram.Client) tg.NewMessageHandler {
	dialogsUsers := newUsers()

	raw := tg.NewClient(client)
	sender := message.NewSender(raw)
	return func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		if filterMessage(update) {
			return nil
		}

		if m, ok := update.Message.(interface{ GetMessage() string }); ok {
			lg := b.logger.With().
				Str("logger", "dispatcher").
				Str("message", m.GetMessage()).
				Logger()
			lg.Info().
				Msg("Got new message update")
		}

		if dialogsUsers.empty() {
			dialogs, err := retryResult(ctx, func() (tg.MessagesDialogsClass, error) {
				dialogs, err := raw.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
					Limit:      100,
					OffsetPeer: &tg.InputPeerEmpty{},
				})
				if err != nil {
					return nil, errors.Wrap(err, "get dialogs")
				}
				return dialogs, nil
			})
			if err != nil {
				return errors.Wrap(err, "get dialogs")
			}
			if dlg, ok := dialogs.AsModified(); ok {
				dialogsUsers.add(dlg.GetUsers()...)
			}
		}

		switch m := update.Message.(type) {
		case *tg.Message:
			switch peer := m.PeerID.(type) {
			case *tg.PeerUser:
				user := entities.Users[peer.UserID]
				if user == nil {
					user = dialogsUsers.get(peer.UserID)
				}

				b.logger.Info().
					Str("text", m.Message).
					Int64("user_id", user.ID).
					Str("user_first_name", user.FirstName).
					Str("username", user.Username).
					Msg("Got message")

				if err := retry(ctx, func() error {
					_, err := sender.To(user.AsInputPeer()).Text(ctx, m.Message)
					return err
				}); err != nil {
					return errors.Wrap(err, "send message")
				}
				return nil
			}
		}

		return nil
	}
}

// Run setups and starts echo bot.
func (b EchoBot) Run(ctx context.Context) error {
	dispatcher := tg.NewUpdateDispatcher()
	client := b.suite.Client(b.logger, dispatcher)
	dispatcher.OnNewMessage(b.handler(client))

	return client.Run(ctx, func(ctx context.Context) error {
		defer close(b.auth)

		me, err := b.login(ctx, client)
		if err != nil {
			return errors.Wrap(err, "login")
		}

		b.logger.Info().
			Str("user", me.Username).
			Int64("id", me.ID).
			Msg("Logged in")

		select {
		case b.auth <- me:
		case <-ctx.Done():
			return ctx.Err()
		}

		<-ctx.Done()
		return nil
	})
}
