package mtproto

import (
	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
	"github.com/gotd/td/proto"
)

func (c *Conn) handleSessionCreated(b *bin.Buffer) error {
	var s mt.NewSessionCreated
	if err := s.Decode(b); err != nil {
		return errors.Wrap(err, "decode")
	}
	c.gotSession.Signal()

	created := proto.MessageID(s.FirstMsgID).Time()
	now := c.clock.Now()
	c.log.Debug().
		Int64("unique_id", s.UniqueID).
		Int64("first_msg_id", s.FirstMsgID).
		Time("first_msg_time", created.Local()).
		Msg("Session created")

	if (created.Before(now) && now.Sub(created) > maxPast) || created.Sub(now) > maxFuture {
		c.log.Warn().
			Time("first_msg_time", created).
			Time("local", now).
			Dur("time_difference", now.Sub(created)).
			Msg("Local clock needs synchronization")
	}

	c.storeSalt(s.ServerSalt)
	if err := c.handler.OnSession(c.session()); err != nil {
		return errors.Wrap(err, "handler.OnSession")
	}
	return nil
}
