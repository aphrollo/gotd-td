package exchange

import (
	"context"
	"crypto/rsa"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/tdsync"
	"github.com/gotd/td/testutil"
	"github.com/gotd/td/transport"
)

func testExchange(rsaPad bool) func(t *testing.T) {
	return func(t *testing.T) {
		a := require.New(t)
		log := zerolog.New(zerolog.NewTestWriter(t))

		dc := 2
		reader := rand.New(rand.NewSource(1))
		key, err := rsa.GenerateKey(reader, crypto.RSAKeyBits)
		a.NoError(err)
		privateKey := PrivateKey{
			RSA: key,
		}

		i := transport.Intermediate
		client, server := i.Pipe()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		clog := log.With().Str("logger", "client").Logger()
		g := tdsync.NewCancellableGroup(ctx)
		g.Go(func(ctx context.Context) error {
			_, err := NewExchanger(client, dc).
				WithLogger(&clog).
				WithRand(reader).
				Client([]PublicKey{privateKey.Public()}).
				Run(ctx)
			return err
		})

		slog := log.With().Str("logger", "server").Logger()
		g.Go(func(ctx context.Context) error {
			_, err := NewExchanger(server, dc).
				WithLogger(&slog).
				WithRand(reader).
				Server(privateKey).
				Run(ctx)
			return err
		})

		a.NoError(g.Wait())
	}
}

func TestExchange(t *testing.T) {
	t.Run("PQInnerData", testExchange(false))
	t.Run("PQInnerDataDC", testExchange(true))
}

func TestExchangeCorpus(t *testing.T) {
	privateKey := PrivateKey{
		RSA: testutil.RSAPrivateKey(),
	}

	for i, seed := range []string{
		"\xef\x00\x04",
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			dc := 2
			reader := testutil.Rand([]byte(seed))
			log := zerolog.New(zerolog.NewTestWriter(t))

			i := transport.Intermediate
			client, server := i.Pipe()

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			g, gctx := errgroup.WithContext(ctx)
			clog := log.With().Str("logger", "client").Logger()
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
			slog := log.With().Str("logger", "server").Logger()
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

			require.NoError(t, g.Wait())
		})
	}
}
