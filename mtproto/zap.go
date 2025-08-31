package mtproto

import (
	"fmt"

	"github.com/gotd/td/bin"
	"github.com/rs/zerolog"
)

type logType struct {
	ID   uint32
	Name string
}

func (l logType) MarshalZerologObject(e *zerolog.Event) {
	typeIDStr := fmt.Sprintf("0x%x", l.ID)
	e.Str("type_id", typeIDStr)
	if l.Name != "" {
		e.Str("type_name", l.Name)
	}
}

func (c *Conn) logWithBuffer(b *bin.Buffer) *zerolog.Logger {
	l := c.logWithType(b).With().
		Int("size_bytes", b.Len()).
		Logger()

	return &l
}

func (c *Conn) logWithType(b *bin.Buffer) *zerolog.Logger {
	id, err := b.PeekID()
	if err != nil {
		return c.log
	}
	return c.logWithTypeID(id)
}

func (c *Conn) logWithTypeID(id uint32) *zerolog.Logger {
	lt := logType{
		ID:   id,
		Name: c.types.Get(id),
	}
	lg := c.log.With().Interface("type", lt).Logger()
	return &lg
}
