package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
)

func run(ctx context.Context) error {
	logger := zerolog.New(os.Stdout).Level(zerolog.DebugLevel)

	dispatcher := tg.NewUpdateDispatcher()
	return telegram.BotFromEnvironment(ctx, telegram.Options{
		Logger:        &logger,
		UpdateHandler: dispatcher,
	}, func(ctx context.Context, client *telegram.Client) error {
		sender := message.NewSender(tg.NewClient(client))
		dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, u *tg.UpdateNewMessage) error {
			m, ok := u.Message.(*tg.Message)
			if !ok || m.Out {
				return nil
			}

			_, err := sender.Reply(entities, u).Text(ctx, m.Message)
			return err
		})
		return nil
	}, telegram.RunUntilCanceled)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(2)
	}
}
