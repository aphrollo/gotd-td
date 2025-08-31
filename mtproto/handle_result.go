package mtproto

import (
	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
	"github.com/gotd/td/proto"
	"github.com/gotd/td/tgerr"
)

func (c *Conn) handleResult(b *bin.Buffer) error {
	// Response to an RPC query.
	var res proto.Result
	if err := res.Decode(b); err != nil {
		return errors.Wrap(err, "decode")
	}

	// Now b contains result message.
	b.ResetTo(res.Result)

	c.logWithBuffer(b).Debug().Int64("msg_id", res.RequestMessageID).Msg("Handle result")

	// Handling gzipped results.
	id, err := b.PeekID()
	if err != nil {
		return err
	}
	if id == proto.GZIPTypeID {
		content, err := gzip(b)
		if err != nil {
			return errors.Wrap(err, "decompress")
		}

		// Replacing buffer so callback will deal with uncompressed data.
		b = content
		c.logWithBuffer(b).Debug().Int64("msg_id", res.RequestMessageID).Msg("Decompressed")

		// Replacing id with inner id if error is compressed for any reason.
		if id, err = b.PeekID(); err != nil {
			return errors.Wrap(err, "peek id")
		}
	}

	if id == mt.RPCErrorTypeID {
		var rpcErr mt.RPCError
		if err := rpcErr.Decode(b); err != nil {
			return errors.Wrap(err, "error decode")
		}

		c.log.Debug().Int64("msg_id", res.RequestMessageID).
			Int("err_code", rpcErr.ErrorCode).
			Str("err_msg", rpcErr.ErrorMessage).
			Msg("Got error")

		c.rpc.NotifyError(res.RequestMessageID, tgerr.New(rpcErr.ErrorCode, rpcErr.ErrorMessage))

		return nil
	}
	if id == mt.PongTypeID {
		return c.handlePong(b)
	}

	return c.rpc.NotifyResult(res.RequestMessageID, b)
}
