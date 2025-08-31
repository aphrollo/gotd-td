package mtproto

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/go-faster/errors"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/clock"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/exchange"
	"github.com/gotd/td/mtproto/salts"
	"github.com/gotd/td/proto"
	"github.com/gotd/td/rpc"
	"github.com/gotd/td/tdsync"
	"github.com/gotd/td/tmap"
	"github.com/gotd/td/transport"
	"github.com/rs/zerolog"
	"go.uber.org/atomic"
)

// Handler will be called on received message from Telegram.
type Handler interface {
	OnMessage(b *bin.Buffer) error
	OnSession(session Session) error
}

// MessageIDSource is message id generator.
type MessageIDSource interface {
	New(t proto.MessageType) int64
}

// MessageBuf is message id buffer.
type MessageBuf interface {
	Consume(id int64) bool
}

// Cipher handles message encryption and decryption.
type Cipher interface {
	DecryptFromBuffer(k crypto.AuthKey, buf *bin.Buffer) (*crypto.EncryptedMessageData, error)
	Encrypt(key crypto.AuthKey, data crypto.EncryptedMessageData, b *bin.Buffer) error
}

// Dialer is an abstraction for MTProto transport connection creator.
type Dialer func(ctx context.Context) (transport.Conn, error)

// Conn represents a MTProto client to Telegram.
type Conn struct {
	dcID int

	dialer        Dialer
	conn          transport.Conn
	handler       Handler
	rpc           *rpc.Engine
	rsaPublicKeys []exchange.PublicKey
	types         *tmap.Map

	// Wrappers for external world, like current time, logs or PRNG.
	// Should be immutable.
	clock        clock.Clock
	rand         io.Reader
	cipher       Cipher
	log          *zerolog.Logger
	messageID    MessageIDSource
	messageIDBuf MessageBuf // replay attack protection

	// use session() to access authKey, salt or sessionID.
	sessionMux sync.RWMutex
	authKey    crypto.AuthKey
	salt       int64
	sessionID  int64

	// server salts fetched by getSalts.
	salts salts.Salts

	// sentContentMessages is count of created content messages, used to
	// compute sequence number within session.
	sentContentMessages int32
	reqMux              sync.Mutex

	// ackSendChan is queue for outgoing message id's that require waiting for
	// ack from server.
	ackSendChan  chan int64
	ackBatchSize int
	ackInterval  time.Duration

	// callbacks for ping results.
	// Key is ping id.
	ping    map[int64]chan struct{}
	pingMux sync.Mutex
	// pingTimeout sets ping_delay_disconnect delay.
	pingTimeout time.Duration
	// pingInterval is duration between ping_delay_disconnect request.
	pingInterval time.Duration

	// gotSession is a signal channel for wait for handleSessionCreated message.
	gotSession *tdsync.Ready

	// exchangeLock locks write calls during key exchange.
	exchangeLock sync.RWMutex

	// compressThreshold is a threshold in bytes to determine that message
	// is large enough to be compressed using gzip.
	compressThreshold int
	dialTimeout       time.Duration
	exchangeTimeout   time.Duration
	saltFetchInterval time.Duration
	getTimeout        func(req uint32) time.Duration
	// Ensure Run once.
	ran atomic.Bool
}

// New creates new unstarted connection.
func New(dialer Dialer, opt Options) *Conn {
	// Set default values, if user does not set.
	opt.setDefaults()

	conn := &Conn{
		dcID: opt.DC,

		dialer:       dialer,
		clock:        opt.Clock,
		rand:         opt.Random,
		cipher:       opt.Cipher,
		log:          opt.Logger,
		messageID:    opt.MessageID,
		messageIDBuf: proto.NewMessageIDBuf(100),

		ackSendChan:  make(chan int64),
		ackInterval:  opt.AckInterval,
		ackBatchSize: opt.AckBatchSize,

		rsaPublicKeys: opt.PublicKeys,
		handler:       opt.Handler,
		types:         opt.Types,

		authKey: opt.Key,
		salt:    opt.Salt,

		ping:         map[int64]chan struct{}{},
		pingTimeout:  opt.PingTimeout,
		pingInterval: opt.PingInterval,

		gotSession: tdsync.NewReady(),

		rpc:               opt.engine,
		compressThreshold: opt.CompressThreshold,
		dialTimeout:       opt.DialTimeout,
		exchangeTimeout:   opt.ExchangeTimeout,
		saltFetchInterval: opt.SaltFetchInterval,
		getTimeout:        opt.RequestTimeout,
	}
	if conn.rpc == nil {
		l := opt.Logger.With().
			Str("logger", "rpc").
			Logger()
		conn.rpc = rpc.New(conn.writeContentMessage, rpc.Options{
			Logger:        &l,
			RetryInterval: opt.RetryInterval,
			MaxRetries:    opt.MaxRetries,
			Clock:         opt.Clock,
			DropHandler:   conn.dropRPC,
		})
	}

	return conn
}

// handleClose closes rpc engine and underlying connection on context done.
func (c *Conn) handleClose(ctx context.Context) error {
	<-ctx.Done()
	c.log.Debug().Msg("Closing")

	c.rpc.ForceClose()
	if err := c.conn.Close(); err != nil {
		c.log.Debug().Err(err).Msg("Failed to cleanup connection")
	}
	return nil
}

// Run initializes MTProto connection to server and blocks until disconnection.
//
// When connection is ready, Handler.OnSession is called.
func (c *Conn) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if c.ran.Swap(true) {
		return errors.New("do Run on closed connection")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.log.Debug().Msg("Run: start")
	defer c.log.Debug().Msg("Run: end")

	if err := c.connect(ctx); err != nil {
		return errors.Wrap(err, "start")
	}
	lg := c.log.With().Str("logger", "group").Logger()
	g := tdsync.NewLogGroup(ctx, &lg)
	g.Go("handleClose", c.handleClose)
	g.Go("pingLoop", c.pingLoop)
	g.Go("ackLoop", c.ackLoop)
	g.Go("saltsLoop", c.saltLoop)
	g.Go("userCallback", f)
	g.Go("readLoop", c.readLoop)

	if err := g.Wait(); err != nil {
		return errors.Wrap(err, "group")
	}

	return nil
}
