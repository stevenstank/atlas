package bench

// Puzzle8 is the 8-puzzle for B3. A state is the 3×3 board in row-major
// order, with 0 for the blank. The single initial state is the solved board
// 1 2 3 / 4 5 6 / 7 8 _ (BENCHMARKS.md fixes the size but not the start;
// every start reaches 9!/2 = 181,440 boards, TESTING.md §1). An action is the
// direction the blank moves; Next emits Up, Down, Left, Right, in that order,
// when the move stays on the board. There is no invariant.
type Puzzle8 struct{}

// Board is a Puzzle8 state.
type Board [9]uint8

func (Puzzle8) Init(emit func(Board) bool) { emit(Board{1, 2, 3, 4, 5, 6, 7, 8, 0}) }

func (Puzzle8) Next(b Board, emit func(string, Board) bool) {
	blank := 0
	for b[blank] != 0 {
		blank++
	}
	r, c := blank/3, blank%3
	for _, mv := range [...]struct {
		name string
		ok   bool
		to   int
	}{
		{"Up", r > 0, blank - 3},
		{"Down", r < 2, blank + 3},
		{"Left", c > 0, blank - 1},
		{"Right", c < 2, blank + 1},
	} {
		if !mv.ok {
			continue
		}
		t := b
		t[blank], t[mv.to] = t[mv.to], 0
		if !emit(mv.name, t) {
			return
		}
	}
}

func (Puzzle8) AppendKey(buf []byte, b Board) []byte { return append(buf, b[:]...) }
