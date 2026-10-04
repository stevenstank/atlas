// Package models holds the small reference models from docs/CONFORMANCE.md.
package models

// Jug is a water-jug state (B, S): litres in the big 5 L jug and the small
// 3 L jug.
type Jug struct{ B, S int }

// Jugs is the water-jug model. Actions are emitted only when they change the
// state, in the order FillBig, FillSmall, EmptyBig, EmptySmall, BigToSmall,
// SmallToBig.
type Jugs struct{}

func (Jugs) Init(emit func(Jug) bool) { emit(Jug{}) }

func (Jugs) Next(j Jug, emit func(string, Jug) bool) {
	b, s := j.B, j.S
	toSmall, toBig := min(b, 3-s), min(s, 5-b)
	steps := [...]struct {
		ok bool
		a  string
		t  Jug
	}{
		{b < 5, "FillBig", Jug{5, s}},
		{s < 3, "FillSmall", Jug{b, 3}},
		{b > 0, "EmptyBig", Jug{0, s}},
		{s > 0, "EmptySmall", Jug{b, 0}},
		{b > 0 && s < 3, "BigToSmall", Jug{b - toSmall, s + toSmall}},
		{s > 0 && b < 5, "SmallToBig", Jug{b + toBig, s - toBig}},
	}
	for _, st := range steps {
		if st.ok && !emit(st.a, st.t) {
			return
		}
	}
}

func (Jugs) AppendKey(buf []byte, j Jug) []byte { return append(buf, byte(j.B), byte(j.S)) }

// NotFour is the water-jug invariant: the big jug never holds exactly 4 L.
func NotFour(j Jug) bool { return j.B != 4 }

// Cell is a Grid2 state.
type Cell struct{ X, Y int }

// Grid2 is the 3×3 grid: IncX if x < 2, then IncY if y < 2. Init emits Inits
// in order; an empty Inits emits nothing (CONFORMANCE.md E1–E2).
type Grid2 struct{ Inits []Cell }

// Origin is the single-initial-state Grid2 of SEMANTICS.md §6.
var Origin = Grid2{Inits: []Cell{{0, 0}}}

// MultiInit is Grid2-MultiInit from CONFORMANCE.md.
var MultiInit = Grid2{Inits: []Cell{{0, 0}, {1, 0}, {0, 0}, {0, 1}, {2, 2}}}

func (g Grid2) Init(emit func(Cell) bool) {
	for _, c := range g.Inits {
		if !emit(c) {
			return
		}
	}
}

func (Grid2) Next(c Cell, emit func(string, Cell) bool) {
	if c.X < 2 && !emit("IncX", Cell{c.X + 1, c.Y}) {
		return
	}
	if c.Y < 2 {
		emit("IncY", Cell{c.X, c.Y + 1})
	}
}

func (Grid2) AppendKey(buf []byte, c Cell) []byte { return append(buf, byte(c.X), byte(c.Y)) }

// Sum is the Grid2 invariant x + y < 4.
func Sum(c Cell) bool { return c.X+c.Y < 4 }
