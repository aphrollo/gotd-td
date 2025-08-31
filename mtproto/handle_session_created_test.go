package mtproto

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/gotd/neo"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
	"github.com/gotd/td/proto"
	"github.com/gotd/td/tdsync"
)

func TestConn_handleSessionCreated(t *testing.T) {
	t.Run("NeedSynchronization", func(t *testing.T) {
		a := require.New(t)

		logs := &bytes.Buffer{}
		logger := zerolog.New(logs).Level(zerolog.WarnLevel).With().Timestamp().Logger()

		now := time.Unix(100, 0)
		clock := neo.NewTime(now)
		gotSession := tdsync.NewReady()
		conn := Conn{
			clock:      clock,
			log:        &logger,
			gotSession: gotSession,
			handler:    newTestHandler(),
		}

		buf := bin.Buffer{}
		msgID := proto.NewMessageID(now.Add(maxFuture+time.Second), proto.MessageFromClient)
		a.NoError(buf.Encode(&mt.NewSessionCreated{
			FirstMsgID: int64(msgID),
			UniqueID:   10,
			ServerSalt: 10,
		}))
		a.NoError(conn.handleSessionCreated(&buf))

		select {
		case <-gotSession.Ready():
		default:
			t.Fatal("expected gotSession signal")
		}
		a.Equal(int64(10), conn.salt)

		// Parse logged lines
		lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
		a.Len(lines, 1)

		var entry map[string]interface{}
		a.NoError(json.Unmarshal([]byte(lines[0]), &entry))
		a.Equal("Local clock needs synchronization", entry["message"])
	})

	t.Run("Invalid", func(t *testing.T) {
		conn := Conn{}
		buf := bin.Buffer{}
		buf.PutID(mt.NewSessionCreatedTypeID)
		require.Error(t, conn.handleSessionCreated(&buf))
	})
}
