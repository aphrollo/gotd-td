package peers_test

import (
	"context"
	"os"

	"github.com/go-faster/errors"
	"github.com/rs/zerolog"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
)

func ExampleManager() {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout})
	clg := logger.With().Str("logger", "client").Logger()
	glg := logger.With().Str("logger", "gaps").Logger()

	var (
		dispatcher = tg.NewUpdateDispatcher()
		h          telegram.UpdateHandler
	)
	client, err := telegram.ClientFromEnvironment(telegram.Options{
		Logger: &clg,
		UpdateHandler: telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
			return h.Handle(ctx, u)
		}),
	})
	if err != nil {
		panic(err)
	}
	peerManager := peers.Options{
		Logger: &logger,
	}.Build(client.API())
	gaps := updates.New(updates.Config{
		Handler:      dispatcher,
		AccessHasher: peerManager,
		Logger:       &glg,
	})
	h = peerManager.UpdateHook(gaps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := client.Run(ctx, func(ctx context.Context) error {
		if err := peerManager.Init(ctx); err != nil {
			return err
		}
		u, err := peerManager.Self(ctx)
		if err != nil {
			return err
		}

		_, isBot := u.ToBot()
		if err := gaps.Run(ctx, client.API(), u.ID(), updates.AuthOptions{IsBot: isBot}); err != nil {
			return errors.Wrap(err, "gaps")
		}
		return nil
	}); err != nil {
		panic(err)
	}
}
