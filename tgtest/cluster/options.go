package cluster

import (
	"io"

	"github.com/gotd/td/crypto"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/transport"
	"github.com/rs/zerolog"
)

// Options of Cluster.
type Options struct {
	// Web denotes to use websocket listener.
	Web bool
	// Random is random source. Used to generate RSA keys.
	// Defaults to rand.Reader.
	Random io.Reader
	// Logger is instance of zerolog.Logger. No logs by default.
	Logger *zerolog.Logger
	// Codec constructor.
	// Defaults to nil (underlying transport server detects protocol automatically).
	Protocol dcs.Protocol
	// Config is an initial cluster config.
	Config tg.Config
	// CDNConfig is an initial cluster CDN config.
	CDNConfig tg.CDNConfig
}

func (opt *Options) setDefaults() {
	// It's okay to use zero value Web.
	if opt.Random == nil {
		opt.Random = crypto.DefaultRand()
	}
	if opt.Logger == nil {
		nop := zerolog.Nop()
		opt.Logger = &nop
	}
	if opt.Protocol == nil {
		opt.Protocol = transport.Intermediate
	}
	// It's okay to use zero value Config.
	// It's okay to use zero value CDNConfig.
}
