package rpc

import (
	"sync"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"

	"github.com/gotd/neo"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/mt"
	"github.com/gotd/td/testutil"
)

type request struct {
	MsgID int64
	SeqNo int32
	Input bin.Encoder
}

var defaultNow = testutil.Date()

const (
	msgID  int64 = 1
	pingID int64 = 1337
	seqNo  int32 = 1
)

func TestRPCError(t *testing.T) {
	clock := neo.NewTime(defaultNow)
	observer := clock.Observe()
	expectedErr := errors.New("server side error")
	log := zerolog.New(zerolog.NewTestWriter(t))

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		log.Info().Msg("Waiting ping request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)

		log.Info().Msg("Got ping request")

		// Make sure that client calls time.After
		// before time travel
		<-observer

		log.Info().Msg("Traveling into the future for a second (simulate job)")
		clock.Travel(time.Second)

		log.Info().Msg("Sending RPC error")
		e.NotifyError(msgID, expectedErr)

		return nil
	}

	client := func(t *testing.T, e *Engine) error {
		log := log.With().Str("logger", "client").Logger()

		log.Info().Msg("Sending ping request")
		err := e.Do(context.TODO(), Request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{
				PingID: pingID,
			},
		})

		log.Info().Msg("Got pong response")
		require.True(t, errors.Is(err, expectedErr), "expected error")

		return nil
	}
	rlg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 3,
		MaxRetries:    2,
		Clock:         clock,
		Logger:        &rlg,
	}, server, client)

}

func TestRPCResult(t *testing.T) {
	clock := neo.NewTime(defaultNow)
	observer := clock.Observe()
	log := zerolog.New(zerolog.NewTestWriter(t))

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		log.Info().Msg("Waiting ping request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)

		log.Info().Msg("Got ping request")
		// Make sure that engine calls time.After
		// before time travel.
		<-observer

		log.Info().Msg("Traveling into the future for 2 seconds (simulate job)")
		clock.Travel(time.Second * 2)

		var b bin.Buffer
		if err := b.Encode(&mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}); err != nil {
			return err
		}

		log.Info().Msg("Sending pong response")
		return e.NotifyResult(msgID, &b)
	}

	client := func(t *testing.T, e *Engine) error {
		log := log.With().Str("logger", "client").Logger()

		log.Info().Msg("Sending ping request")
		var out mt.Pong
		require.NoError(t, e.Do(context.TODO(), Request{
			MsgID:  msgID,
			SeqNo:  seqNo,
			Input:  &mt.PingRequest{PingID: pingID},
			Output: &out,
		}))

		log.Info().Msg("Got pong response")
		require.Equal(t, mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}, out)

		return nil
	}

	lg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 4,
		MaxRetries:    2,
		Clock:         clock,
		Logger:        &lg,
	}, server, client)

}

func TestRPCAckThenResult(t *testing.T) {
	clock := neo.NewTime(defaultNow)
	observer := clock.Observe()
	log := zerolog.New(zerolog.NewTestWriter(t))

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		log.Info().Msg("Waiting ping request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)

		// Make sure that client calls time.After
		// before time travel.
		<-observer

		log.Info().Msg("Traveling into the future for 2 seconds (simulate job)")
		clock.Travel(time.Second * 2)

		log.Info().Msg("Sending ACK")
		e.NotifyAcks([]int64{msgID})

		log.Info().Msg("Traveling into the future for 6 seconds (simulate request processing)")
		clock.Travel(time.Second * 6)

		var b bin.Buffer
		if err := b.Encode(&mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}); err != nil {
			return err
		}

		log.Info().Msg("Sending response")
		return e.NotifyResult(msgID, &b)
	}

	client := func(t *testing.T, e *Engine) error {
		log := log.With().Str("logger", "client").Logger()

		log.Info().Msg("Sending ping request")
		var out mt.Pong
		require.NoError(t, e.Do(context.TODO(), Request{
			MsgID:  msgID,
			SeqNo:  seqNo,
			Input:  &mt.PingRequest{PingID: pingID},
			Output: &out,
		}))

		log.Info().Msg("Got pong response")
		require.Equal(t, mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}, out)

		return nil
	}

	lg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 4,
		MaxRetries:    2,
		Clock:         clock,
		Logger:        &lg,
	}, server, client)
}

func TestRPCWithRetryResult(t *testing.T) {
	clock := neo.NewTime(defaultNow)
	observer := clock.Observe()
	log := zerolog.New(zerolog.NewTestWriter(t))

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		log.Info().Msg("Waiting ping request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)
		log.Info().Msg("Got ping request")

		// Make sure that client calls time.After
		// before time travel.
		<-observer

		log.Info().Msg("Traveling into the future for 6 seconds (simulate request loss)")
		clock.Travel(time.Second * 6)

		log.Info().Msg("Waiting re-sending request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)
		log.Info().Msg("Got ping request")

		var b bin.Buffer
		if err := b.Encode(&mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}); err != nil {
			return err
		}

		log.Info().Msg("Send pong response")
		return e.NotifyResult(msgID, &b)
	}

	client := func(t *testing.T, e *Engine) error {
		log := log.With().Str("logger", "client").Logger()

		log.Info().Msg("Sending ping request")
		var out mt.Pong
		require.NoError(t, e.Do(context.TODO(), Request{
			MsgID:  msgID,
			SeqNo:  seqNo,
			Input:  &mt.PingRequest{PingID: pingID},
			Output: &out,
		}))

		log.Info().Msg("Got pong response")
		require.Equal(t, mt.Pong{
			MsgID:  msgID,
			PingID: pingID,
		}, out)

		return nil
	}

	lg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 4,
		MaxRetries:    5,
		Clock:         clock,
		Logger:        &lg,
	}, server, client)

}

func TestEngineGracefulShutdown(t *testing.T) {
	var (
		log             = zerolog.New(zerolog.NewTestWriter(t))
		expectedErr     = errors.New("server side error")
		requestsCount   = 10
		serverRecv      sync.WaitGroup
		canSendResponse sync.Mutex
	)

	serverRecv.Add(requestsCount)
	canSendResponse.Lock()

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		var batch []request
		for i := 0; i < requestsCount; i++ {
			batch = append(batch, <-incoming)
			serverRecv.Done()
		}
		e.log.Info().Msg("Got all requests")

		canSendResponse.Lock()
		e.log.Info().Msg("Sending responses")
		for _, req := range batch {
			log.Info().Msg("send response")
			e.NotifyError(req.MsgID, expectedErr)
		}
		canSendResponse.Unlock()

		return nil
	}

	client := func(t *testing.T, e *Engine) error {
		var currMsgID int64

		for i := 0; i < requestsCount; i++ {
			go func(t *testing.T, msgID int64) {
				var out mt.Pong
				require.Equal(t, e.Do(context.TODO(), Request{
					MsgID:  msgID,
					SeqNo:  seqNo,
					Input:  &mt.PingRequest{PingID: pingID},
					Output: &out,
				}), expectedErr)
			}(t, currMsgID)

			currMsgID++
		}

		// wait until server receive all requests
		serverRecv.Wait()
		// allow server to send responses
		canSendResponse.Unlock()
		// close the engine
		e.Close()

		return nil
	}

	lg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 5,
		MaxRetries:    5,
		Logger:        &lg,
	}, server, client)
}

func TestDropRPC(t *testing.T) {
	clock := neo.NewTime(defaultNow)
	log := zerolog.New(zerolog.NewTestWriter(t))
	serverRecvRequest := make(chan struct{})
	clientCancelledCtx := make(chan struct{})
	dropChan := make(chan Request)

	server := func(t *testing.T, e *Engine, incoming <-chan request) error {
		log := log.With().Str("logger", "server").Logger()

		log.Info().Msg("Waiting ping request")
		require.Equal(t, request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: &mt.PingRequest{PingID: pingID},
		}, <-incoming)

		close(serverRecvRequest)
		<-clientCancelledCtx

		log.Info().Msg("Waiting drop request")
		require.Equal(t, msgID, (<-dropChan).MsgID)
		return nil
	}

	client := func(t *testing.T, e *Engine) error {
		log := log.With().Str("logger", "client").Logger()

		log.Info().Msg("Sending ping request")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			<-serverRecvRequest
			log.Info().Msg("Canceling request context")
			cancel()
			close(clientCancelledCtx)
		}()

		require.ErrorIs(t, e.Do(ctx, Request{
			MsgID:  msgID,
			SeqNo:  seqNo,
			Input:  &mt.PingRequest{PingID: pingID},
			Output: &mt.Pong{},
		}), context.Canceled)

		return nil
	}

	lg := log.With().Str("logger", "rpc").Logger()
	runTest(t, Options{
		RetryInterval: time.Second * 4,
		MaxRetries:    2,
		Clock:         clock,
		Logger:        &lg,
		DropHandler:   func(req Request) error { dropChan <- req; return nil },
	}, server, client)

}

func runTest(
	t *testing.T,
	cfg Options,
	server func(t *testing.T, e *Engine, incoming <-chan request) error,
	client func(t *testing.T, e *Engine) error,
) {
	t.Helper()

	// Channel of client requests sent to the server.
	requests := make(chan request)
	defer close(requests)

	e := New(func(ctx context.Context, msgID int64, seqNo int32, in bin.Encoder) error {
		req := request{
			MsgID: msgID,
			SeqNo: seqNo,
			Input: in,
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case requests <- req:
			return nil
		}
	}, cfg)

	var g errgroup.Group
	g.Go(func() error { return server(t, e, requests) })
	g.Go(func() error { return client(t, e) })

	require.NoError(t, g.Wait())
	e.Close()
	//require.NoError(t, cfg.Logger.Err())
}
