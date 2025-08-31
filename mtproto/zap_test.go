package mtproto

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tmap"
)

func BenchmarkConn_logWithType(b *testing.B) {
	nop := zerolog.Nop()
	c := Conn{
		log: &nop,
		types: tmap.New(map[uint32]string{
			0x3fedd339: "true",
		}),
	}
	buf := bin.Buffer{}
	buf.PutID(0x3fedd339)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.logWithType(&buf).Info().Msg("Hi!")
	}
}
