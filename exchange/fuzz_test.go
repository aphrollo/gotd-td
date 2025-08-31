//go:build go1.18

package exchange

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/gotd/td/testutil"
	"github.com/gotd/td/transport"
)

func FuzzValid(f *testing.F) {
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		const dc = 2
		reader := testutil.Rand(data)
		privateKey := PrivateKey{
			RSA: testutil.RSAPrivateKey(),
		}

		logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).Level(zerolog.InfoLevel)

		i := transport.Intermediate
		client, server := i.Pipe()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		g, gctx := errgroup.WithContext(ctx)
		clog := logger.With().Str("logger", "client").Logger()
		g.Go(func() error {
			_, err := NewExchanger(client, dc).
				WithLogger(&clog).
				WithRand(reader).
				Client([]PublicKey{privateKey.Public()}).
				Run(gctx)
			if err != nil {
				cancel()
			}
			return err
		})
		slog := logger.With().Str("logger", "server").Logger()
		g.Go(func() error {
			_, err := NewExchanger(server, dc).
				WithLogger(&slog).
				WithRand(reader).
				Server(privateKey).
				Run(gctx)
			if err != nil {
				cancel()
			}
			return err
		})

		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
	})
}
