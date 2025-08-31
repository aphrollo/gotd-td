package tgtest

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

type testTransportHandler struct {
	t      testing.TB
	logger *zerolog.Logger
	// For ACK testing proposes.
	// We send ack only after second request
	counter   int
	counterMx sync.Mutex

	message string // immutable
}

// TestTransport is a handler for testing MTProto transport.
func TestTransport(t testing.TB, logger *zerolog.Logger, message string) Handler {
	return &testTransportHandler{
		t:       t,
		logger:  logger,
		message: message,
	}
}

func (h *testTransportHandler) OnMessage(server *Server, req *Request) error {
	id, err := req.Buf.PeekID()
	if err != nil {
		return err
	}

	h.logger.Info().Str("id", fmt.Sprintf("%#x", id)).Msg("New message")

	switch id {
	case tg.UsersGetUsersRequestTypeID:
		getUsers := tg.UsersGetUsersRequest{}

		if err := getUsers.Decode(req.Buf); err != nil {
			return err
		}
		h.logger.Info().Msg("New client connected, invoke received")

		if err := server.SendVector(req, &tg.User{
			ID:         10,
			AccessHash: 10,
			Username:   "rustcocks",
		}); err != nil {
			return err
		}

		h.logger.Info().Str("message", h.message).Msg("Sending message")
		return server.SendUpdates(req.RequestCtx, req.Session, &tg.UpdateNewMessage{
			Message: &tg.Message{
				ID:      1,
				PeerID:  &tg.PeerUser{UserID: 1},
				Message: h.message,
			},
		})
	case tg.MessagesSendMessageRequestTypeID:
		m := &tg.MessagesSendMessageRequest{}
		if err := m.Decode(req.Buf); err != nil {
			h.t.Fail()
			return err
		}

		assert.Equal(h.t, "какими деньгами?", m.Message)

		h.counterMx.Lock()
		h.counter++
		if h.counter < 2 {
			h.counterMx.Unlock()
			return nil
		}
		h.counterMx.Unlock()

		return server.SendResult(req, &tg.Updates{})
	}

	return nil
}
