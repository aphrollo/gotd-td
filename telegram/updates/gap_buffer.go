package updates

import (
	"github.com/rs/zerolog"
)

type gap struct {
	from, to int
}

type gapBuffer struct {
	gaps []gap
}

func (b gapBuffer) Has() bool { return len(b.gaps) > 0 }

func (b *gapBuffer) Clear() { b.gaps = make([]gap, 0, 1) }

func (b *gapBuffer) Enable(from, to int) {
	if len(b.gaps) > 0 {
		panic("unreachable")
	}

	b.gaps = append(b.gaps, gap{from, to})
}

func (b *gapBuffer) Consume(u update) (accepted bool) {
	for i, g := range b.gaps {
		if g.from <= u.start() && g.to >= u.end() {
			if g.from < u.start() {
				b.gaps = append(b.gaps, gap{from: g.from, to: u.start()})
			}
			if g.to > u.end() {
				b.gaps = append(b.gaps, gap{from: u.end(), to: g.to})
			}

			b.gaps = append(b.gaps[:i], b.gaps[i+1:]...)
			return true
		}
	}

	return false
}

func (b gapBuffer) MarshalZerologArray(a *zerolog.Array) {
	for _, g := range b.gaps {
		a.Object(&gapMarshaler{from: g.from, to: g.to})
	}
}

type gapMarshaler struct {
	from int
	to   int
}

func (g *gapMarshaler) MarshalZerologObject(e *zerolog.Event) {
	e.Int("from", g.from).Int("to", g.to)
}
