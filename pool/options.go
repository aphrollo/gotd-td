package pool

import (
	"github.com/rs/zerolog"
)

// DCOptions is a Telegram data center connections pool options.
type DCOptions struct {
	// Logger is instance of zerolog.Logger. No logs by default.
	Logger *zerolog.Logger
	// MTProto options for connections.
	// Opened connection limit to the DC.
	MaxOpenConnections int64
}

func (d *DCOptions) setDefaults() {
	if d.Logger == nil {
		nop := zerolog.Nop()
		d.Logger = &nop
	}
	// It's okay to use zero value for MaxOpenConnections.
}
