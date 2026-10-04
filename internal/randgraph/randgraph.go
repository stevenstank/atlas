// Package randgraph generates small random models for property and fuzz
// tests (docs/TESTING.md §8–9).
package randgraph

import "math/rand/v2"

// Graph is a model over nodes 0..N-1: initial nodes in order (repeats
// allowed), adjacency lists that may hold self-loops and duplicate edges, and
// a bitmask of violating nodes. The action is the edge index, so duplicate
// edges are distinct (action, successor) pairs.
type Graph struct {
	N     int
	Inits []int
	Adj   [][]int
	Bad   uint8
}

func (g Graph) Init(emit func(int) bool) {
	for _, v := range g.Inits {
		if !emit(v) {
			return
		}
	}
}

func (g Graph) Next(v int, emit func(int, int) bool) {
	for i, w := range g.Adj[v] {
		if !emit(i, w) {
			return
		}
	}
}

func (Graph) AppendKey(buf []byte, v int) []byte { return append(buf, byte(v)) }

// Good is the invariant: v is not in Bad.
func (g Graph) Good(v int) bool { return g.Bad>>v&1 == 0 }

// Decode turns bytes into a graph with at most 8 nodes, 3 initial emissions,
// and 3 edges per node, plus a depth limit (-1 or 0..7) and a state limit
// (-1 or 1..11), where -1 means none. Missing bytes read as 0.
func Decode(data []byte) (g Graph, depth, states int) {
	next := func() int {
		if len(data) == 0 {
			return 0
		}
		b := data[0]
		data = data[1:]
		return int(b)
	}
	g.N = 1 + next()%8
	for range next() % 4 {
		g.Inits = append(g.Inits, next()%g.N)
	}
	g.Adj = make([][]int, g.N)
	for v := range g.N {
		for range next() % 4 {
			g.Adj[v] = append(g.Adj[v], next()%g.N)
		}
	}
	g.Bad = uint8(next())
	depth, states = next()%10-1, next()%12
	if depth == 8 {
		depth = -1
	}
	if states == 0 {
		states = -1
	}
	return g, depth, states
}

// Encode builds bytes that Decode turns back into the given graph and limits.
func Encode(n int, inits []int, adj [][]int, bad uint8, depth, states int) []byte {
	b := []byte{byte(n - 1), byte(len(inits))}
	for _, v := range inits {
		b = append(b, byte(v))
	}
	for _, out := range adj {
		b = append(b, byte(len(out)))
		for _, w := range out {
			b = append(b, byte(w))
		}
	}
	return append(b, bad, byte(depth+1), byte(max(states, 0)))
}

// Bytes returns the deterministic input for seed, built from explicit
// distributions so that limits and refusals are common: 1–8 nodes; 1–3
// initial emissions (none in about 1 seed in 20); 0–3 edges per node; each
// node violating with probability 1/8; a depth limit of 0–3 and a state
// limit of 1–6, each set in 40% of seeds (small enough to bind on graphs of
// at most 8 nodes). A failing seed reproduces exactly.
func Bytes(seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, 0x61746c6173)) // "atlas"
	n := 1 + r.IntN(8)
	inits := make([]int, 1+r.IntN(3))
	if r.IntN(20) == 0 {
		inits = nil
	}
	for i := range inits {
		inits[i] = r.IntN(n)
	}
	adj := make([][]int, n)
	var bad uint8
	for v := range adj {
		adj[v] = make([]int, r.IntN(4))
		for i := range adj[v] {
			adj[v][i] = r.IntN(n)
		}
		if r.IntN(8) == 0 {
			bad |= 1 << v
		}
	}
	depth, states := -1, -1
	if r.IntN(5) < 2 {
		depth = r.IntN(4)
	}
	if r.IntN(5) < 2 {
		states = 1 + r.IntN(6)
	}
	return Encode(n, inits, adj, bad, depth, states)
}
