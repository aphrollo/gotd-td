package mtproto

import (
	"context"
	"crypto/rand"
	"io"
	"testing"
	"testing/iotest"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/clock"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/transport"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type closeConn struct {
	closed bool
}

func (c *closeConn) Send(ctx context.Context, b *bin.Buffer) error {
	return io.EOF
}

func (c *closeConn) Recv(ctx context.Context, b *bin.Buffer) error {
	return io.EOF
}

func (c *closeConn) Close() error {
	c.closed = true
	return nil
}

func TestConn_connect(t *testing.T) {
	t.Run("EnsureClose", func(t *testing.T) {
		t.Run("Exchange", func(t *testing.T) {
			a := require.New(t)

			nop := zerolog.Nop()
			closeMe := &closeConn{}
			c := Conn{
				dialer: func(ctx context.Context) (transport.Conn, error) {
					return closeMe, nil
				},
				clock: clock.System,
				rand:  rand.Reader,
				log:   &nop,
			}

			a.Error(c.connect(context.Background()))
			a.True(closeMe.closed)
		})
		t.Run("SessionID", func(t *testing.T) {
			a := require.New(t)

			nop := zerolog.Nop()
			closeMe := &closeConn{}
			c := Conn{
				dialer: func(ctx context.Context) (transport.Conn, error) {
					return closeMe, nil
				},
				clock: clock.System,
				authKey: crypto.AuthKey{
					ID: [8]byte{1}, // Skip exchange.
				},
				rand: iotest.ErrReader(io.EOF),
				log:  &nop,
			}

			a.Error(c.connect(context.Background()))
			a.True(closeMe.closed)
		})
	})
}
