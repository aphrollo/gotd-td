package tgtest

import (
	"encoding/hex"

	"github.com/gotd/td/crypto"
	"github.com/rs/zerolog"
)

// Session represents connection session.
type Session struct {
	// ID is a Session ID.
	ID int64
	// AuthKey is an attached key.
	AuthKey crypto.AuthKey
}

// MarshalZerologObject implements zerolog.LogObjectMarshaler.
func (s Session) MarshalZerologObject(e *zerolog.Event) {
	e.Int64("session_id", s.ID).
		Str("key_id", hex.EncodeToString(s.AuthKey.ID[:]))
}
