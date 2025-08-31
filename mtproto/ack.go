package mtproto

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/gotd/td/mt"
)

func (c *Conn) ackLoop(ctx context.Context) error {
	log := c.log.With().
		Str("logger", "ack").
		Logger()

	var buf []int64
	send := func() {
		defer func() { buf = buf[:0] }()

		if err := c.writeServiceMessage(ctx, &mt.MsgsAck{MsgIDs: buf}); err != nil {
			c.log.Error().Err(err).Msg("Failed to ACK")
			return
		}

		log.Debug().
			Ints64("msg_ids", buf).
			Msg("Ack")

	}

	ticker := c.clock.Ticker(c.ackInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "acl")
		case <-ticker.C():
			if len(buf) > 0 {
				send()
			}
		case msgID := <-c.ackSendChan:
			buf = append(buf, msgID)
			if len(buf) >= c.ackBatchSize {
				send()
				ticker.Reset(c.ackInterval)
			}
		}
	}
}
