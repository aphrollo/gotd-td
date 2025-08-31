package mtproto

import (
	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
)

func (c *Conn) handleAck(b *bin.Buffer) error {
	var ack mt.MsgsAck
	if err := ack.Decode(b); err != nil {
		return errors.Wrap(err, "decode")
	}

	c.log.Debug().Ints64("msg_ids", ack.MsgIDs).Msg("Received ack")
	c.rpc.NotifyAcks(ack.MsgIDs)

	return nil
}
