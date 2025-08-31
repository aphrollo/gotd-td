package exchange

import (
	"io"

	"github.com/gotd/td/crypto"
	"github.com/rs/zerolog"
)

// ClientExchange is a client-side key exchange flow.
type ClientExchange struct {
	unencryptedWriter
	rand io.Reader
	log  *zerolog.Logger

	keys []PublicKey
	dc   int
}

// ClientExchangeResult contains client part of key exchange result.
type ClientExchangeResult struct {
	AuthKey    crypto.AuthKey
	SessionID  int64
	ServerSalt int64
}
