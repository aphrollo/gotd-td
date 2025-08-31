package updates

import (
	"context"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// API is the interface which contains
// Telegram RPC methods used by manager for internalState synchronization.
type API interface {
	UpdatesGetState(ctx context.Context) (*tg.UpdatesState, error)
	UpdatesGetDifference(ctx context.Context, request *tg.UpdatesGetDifferenceRequest) (tg.UpdatesDifferenceClass, error)
	UpdatesGetChannelDifference(ctx context.Context, request *tg.UpdatesGetChannelDifferenceRequest) (tg.UpdatesChannelDifferenceClass, error)
}

// Config of the manager.
type Config struct {
	// Handler where updates will be passed.
	Handler telegram.UpdateHandler
	// Callback called if manager cannot
	// recover channel gap (optional).
	OnChannelTooLong func(channelID int64)
	// State storage.
	// In-mem used if not provided.
	Storage StateStorage
	// Channel access hash storage.
	// In-mem used if not provided.
	AccessHasher ChannelAccessHasher
	// Logger (optional).
	Logger *zerolog.Logger
	// TracerProvider (optional).
	TracerProvider trace.TracerProvider
}

func (cfg *Config) setDefaults() {
	if cfg.Handler == nil {
		panic("Handler is nil")
	}
	if cfg.AccessHasher == nil {
		cfg.AccessHasher = newMemAccessHasher()
	}
	if cfg.Logger == nil {
		nop := zerolog.Nop()
		cfg.Logger = &nop
	}
	if cfg.TracerProvider == nil {
		cfg.TracerProvider = trace.NewNoopTracerProvider()
	}
	if cfg.Storage == nil {
		cfg.Storage = newMemStorage()
	}
	if cfg.OnChannelTooLong == nil {
		cfg.OnChannelTooLong = func(channelID int64) {
			cfg.Logger.Error().Int64("channel_id", channelID).Msg("Difference too long")
		}
	}
}
