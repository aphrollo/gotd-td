package exchange

import (
	"io"

	"github.com/gotd/td/crypto"
	"github.com/rs/zerolog"
)

// ServerExchange is a server-side key exchange flow.
type ServerExchange struct {
	unencryptedWriter
	rand io.Reader
	log  *zerolog.Logger

	rng ServerRNG
	key PrivateKey
	dc  int
}

// ServerExchangeResult contains server part of key exchange result.
type ServerExchangeResult struct {
	Key        crypto.AuthKey
	ServerSalt int64
}
