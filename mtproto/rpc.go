package mtproto

import (
	"context"

	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
	"github.com/gotd/td/rpc"
)

// Invoke sends input and decodes result into output.
//
// NOTE: Assuming that call contains content message (seqno increment).
func (c *Conn) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	msgID, seqNo := c.nextMsgSeq(true)
	req := rpc.Request{
		MsgID:  msgID,
		SeqNo:  seqNo,
		Input:  input,
		Output: output,
	}

	l := c.log.With().
		Int64("msg_id", req.MsgID).
		Logger()
	l.Debug().Msg("Invoke start")
	defer l.Debug().Msg("Invoke end")

	if err := c.rpc.Do(ctx, req); err != nil {
		var badMsgErr *badMessageError
		if errors.As(err, &badMsgErr) && badMsgErr.Code == codeIncorrectServerSalt {
			c.log.Debug().Msg("Setting server salt")
			c.storeSalt(badMsgErr.NewSalt)
			c.salts.Reset()
			c.log.Info().
				Int64("msg_id", req.MsgID).
				Msg("Retrying request after badMsgErr")
			return c.rpc.Do(ctx, req)
		}
		return errors.Wrap(err, "rpcDoRequest")
	}

	return nil
}

func (c *Conn) dropRPC(req rpc.Request) error {
	ctx, cancel := context.WithTimeout(context.Background(),
		c.getTimeout(mt.RPCDropAnswerRequestTypeID),
	)
	defer cancel()

	var resp mt.RPCDropAnswerBox
	if err := c.Invoke(ctx, &mt.RPCDropAnswerRequest{
		ReqMsgID: req.MsgID,
	}, &resp); err != nil {
		return err
	}

	switch resp.RpcDropAnswer.(type) {
	case *mt.RPCAnswerDropped, *mt.RPCAnswerDroppedRunning:
		return nil
	case *mt.RPCAnswerUnknown:
		return errors.New("answer unknown")
	default:
		return errors.Errorf("unexpected response type: %T", resp.RpcDropAnswer)
	}
}
