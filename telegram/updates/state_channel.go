package updates

import (
	"context"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

type channelUpdate struct {
	update   tg.UpdateClass
	entities entities
	span     trace.SpanContext
}

type channelState struct {
	// Updates from *internalState.
	updates chan channelUpdate
	// Channel to pass diff.OtherUpdates into *internalState.
	out chan<- tracedUpdate

	// Channel internalState.
	pts         *sequenceBox
	idleTimeout *time.Timer
	diffTimeout time.Time

	// Immutable fields.
	channelID  int64
	accessHash int64
	selfID     int64
	diffLim    int
	client     API
	storage    StateStorage
	log        *zerolog.Logger
	tracer     trace.Tracer
	handler    telegram.UpdateHandler
	onTooLong  func(channelID int64)
}

type channelStateConfig struct {
	Out              chan tracedUpdate
	InitialPts       int
	ChannelID        int64
	AccessHash       int64
	SelfID           int64
	DiffLimit        int
	RawClient        API
	Storage          StateStorage
	Handler          telegram.UpdateHandler
	OnChannelTooLong func(channelID int64)
	Logger           *zerolog.Logger
	Tracer           trace.Tracer
}

func newChannelState(cfg channelStateConfig) *channelState {
	state := &channelState{
		updates: make(chan channelUpdate, 10),
		out:     cfg.Out,

		idleTimeout: time.NewTimer(idleTimeout),

		channelID:  cfg.ChannelID,
		accessHash: cfg.AccessHash,
		selfID:     cfg.SelfID,
		diffLim:    cfg.DiffLimit,
		client:     cfg.RawClient,
		storage:    cfg.Storage,
		log:        cfg.Logger,
		handler:    cfg.Handler,
		onTooLong:  cfg.OnChannelTooLong,
		tracer:     cfg.Tracer,
	}
	lg := cfg.Logger.With().Str("logger", "pts").Logger()
	state.pts = newSequenceBox(sequenceConfig{
		InitialState: cfg.InitialPts,
		Apply:        state.applyPts,
		Logger:       &lg,
		Tracer:       cfg.Tracer,
	})

	return state
}

func (s *channelState) Push(ctx context.Context, u channelUpdate) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.updates <- u:
		return nil
	}
}

func (s *channelState) Run(ctx context.Context) error {
	// Subscribe to channel updates.
	if err := s.getDifference(ctx); err != nil {
		s.log.Error().Err(err).Msg("Failed to subscribe to channel updates")
	}

	for {
		select {
		case u := <-s.updates:
			ctx := trace.ContextWithSpanContext(ctx, u.span)
			if err := s.handleUpdate(ctx, u.update, u.entities); err != nil {
				s.log.Error().Err(err).Msg("Handle update error")
			}
		case <-s.pts.gapTimeout.C:
			s.log.Debug().Msg("Gap timeout")
			s.getDifferenceLogger(ctx)
		case <-ctx.Done():
			if len(s.pts.pending) > 0 {
				// This will probably fail.
				s.getDifferenceLogger(ctx)
			}
			return ctx.Err()
		case <-s.idleTimeout.C:
			s.log.Debug().Msg("Idle timeout")
			s.resetIdleTimer()
			s.getDifferenceLogger(ctx)
		}
	}
}

func (s *channelState) handleUpdate(ctx context.Context, u tg.UpdateClass, ents entities) error {
	ctx, span := s.tracer.Start(ctx, "channelState.handleUpdate")
	defer span.End()

	s.resetIdleTimer()

	if long, ok := u.(*tg.UpdateChannelTooLong); ok {
		return s.handleTooLong(ctx, long)
	}

	channelID, pts, ptsCount, ok, err := tg.IsChannelPtsUpdate(u)
	if err != nil {
		return errors.Wrap(err, "invalid update")
	}

	if !ok {
		return errors.Errorf("expected channel update, got: %T", u)
	}

	if channelID != s.channelID {
		return errors.Errorf("update for wrong channel (channelID: %d)", channelID)
	}

	return s.pts.Handle(ctx, update{
		Value:    u,
		State:    pts,
		Count:    ptsCount,
		Entities: ents,
	})
}

func (s *channelState) handleTooLong(ctx context.Context, long *tg.UpdateChannelTooLong) error {
	ctx, span := s.tracer.Start(ctx, "channelState.handleTooLong")
	defer span.End()

	remotePts, ok := long.GetPts()
	if !ok {
		s.log.Warn().Msg("Got UpdateChannelTooLong without pts field")
		return s.getDifference(ctx)
	}

	// Note: we still can fetch latest diffLim updates.
	// Should we do?
	if remotePts-s.pts.State() > s.diffLim {
		s.onTooLong(s.channelID)
		return nil
	}

	return s.getDifference(ctx)
}

func (s *channelState) applyPts(ctx context.Context, state int, updates []update) error {
	ctx, span := s.tracer.Start(ctx, "channelState.applyPts")
	defer span.End()

	var (
		converted []tg.UpdateClass
		ents      entities
	)

	for _, update := range updates {
		converted = append(converted, update.Value.(tg.UpdateClass))
		ents.Merge(update.Entities)
	}

	if err := s.handler.Handle(ctx, &tg.Updates{
		Updates: converted,
		Users:   ents.Users,
		Chats:   ents.Chats,
	}); err != nil {
		s.log.Error().Err(err).Msg("Handle update error")
		return nil
	}

	if err := s.storage.SetChannelPts(ctx, s.selfID, s.channelID, state); err != nil {
		s.log.Error().Err(err).Msg("SetChannelPts error")
	}

	return nil
}

func (s *channelState) getDifference(ctx context.Context) error {
	ctx, span := s.tracer.Start(ctx, "channelState.getDifference")
	defer span.End()
	s.pts.gaps.Clear()

	s.log.Debug().Msg("Getting difference")

	if now := time.Now(); now.Before(s.diffTimeout) {
		dur := s.diffTimeout.Sub(now)
		s.log.Debug().Dur("duration", dur).Msg("GetChannelDifference timeout")
		if err := func() error {
			afterC := time.After(dur)
			for {
				select {
				case <-afterC:
					return nil
				case u, ok := <-s.updates:
					if !ok {
						continue
					}

					// Ignoring updates to prevent *internalState worker from blocking.
					// All ignored updates should be restored by future getChannelDifference call.
					// At least I hope so...
					s.log.Debug().Any("update", u.update).Msg("Ignoring update due to getChannelDifference timeout")
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}(); err != nil {
			return err
		}
	}

	diff, err := s.client.UpdatesGetChannelDifference(ctx, &tg.UpdatesGetChannelDifferenceRequest{
		Channel: &tg.InputChannel{
			ChannelID:  s.channelID,
			AccessHash: s.accessHash,
		},
		Filter: &tg.ChannelMessagesFilterEmpty{},
		Pts:    s.pts.State(),
		Limit:  s.diffLim,
	})
	if err != nil {
		return errors.Wrap(err, "get channel difference")
	}

	switch diff := diff.(type) {
	case *tg.UpdatesChannelDifference:
		if len(diff.OtherUpdates) > 0 {
			select {
			case s.out <- tracedUpdate{
				span: trace.SpanContextFromContext(ctx),
				update: &tg.Updates{
					Updates: diff.OtherUpdates,
					Users:   diff.Users,
					Chats:   diff.Chats,
				},
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if len(diff.NewMessages) > 0 {
			if err := s.handler.Handle(ctx, &tg.Updates{
				Updates: msgsToUpdates(diff.NewMessages, true),
				Users:   diff.Users,
				Chats:   diff.Chats,
			}); err != nil {
				s.log.Error().Err(err).Msg("Handle updates error")
			}
		}

		if err := s.storage.SetChannelPts(ctx, s.selfID, s.channelID, diff.Pts); err != nil {
			s.log.Warn().Err(err).Msg("SetChannelPts error")
		}

		s.pts.SetState(diff.Pts, "updates.channelDifference")
		if seconds, ok := diff.GetTimeout(); ok {
			s.diffTimeout = time.Now().Add(time.Second * time.Duration(seconds))
		}

		if !diff.Final {
			return s.getDifference(ctx)
		}

		return nil

	case *tg.UpdatesChannelDifferenceEmpty:
		if err := s.storage.SetChannelPts(ctx, s.selfID, s.channelID, diff.Pts); err != nil {
			s.log.Warn().Err(err).Msg("SetChannelPts error")
		}

		s.pts.SetState(diff.Pts, "updates.channelDifferenceEmpty")
		if seconds, ok := diff.GetTimeout(); ok {
			s.diffTimeout = time.Now().Add(time.Second * time.Duration(seconds))
		}

		return nil

	case *tg.UpdatesChannelDifferenceTooLong:
		if seconds, ok := diff.GetTimeout(); ok {
			s.diffTimeout = time.Now().Add(time.Second * time.Duration(seconds))
		}

		remotePts, err := getDialogPts(diff.Dialog)
		if err != nil {
			s.log.Warn().Err(err).Msg("UpdatesChannelDifferenceTooLong invalid Dialog")
		} else {
			if err := s.storage.SetChannelPts(ctx, s.selfID, s.channelID, remotePts); err != nil {
				s.log.Warn().Err(err).Msg("SetChannelPts error")
			}

			s.pts.SetState(remotePts, "updates.channelDifferenceTooLong dialog new pts")
		}

		s.onTooLong(s.channelID)
		return nil

	default:
		return errors.Errorf("unexpected channel diff type: %T", diff)
	}
}

func (s *channelState) getDifferenceLogger(ctx context.Context) {
	if err := s.getDifference(ctx); err != nil {
		s.log.Error().Err(err).Msg("get channel difference error")
	}
}

func (s *channelState) resetIdleTimer() {
	if len(s.idleTimeout.C) > 0 {
		<-s.idleTimeout.C
	}

	_ = s.idleTimeout.Reset(idleTimeout)
}
