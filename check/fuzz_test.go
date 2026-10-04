package check_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/reference"
)

// graph is a small random model (TESTING.md §8): nodes 0..n-1, initial nodes
// in order (repeats allowed), adjacency lists that may hold self-loops and
// duplicate edges, and a bitmask of violating nodes. The action is the edge
// index, so duplicate edges are distinct (action, successor) pairs.
type graph struct {
	n     int
	inits []int
	adj   [][]int
	bad   uint8
}

func (g graph) Init(emit func(int) bool) {
	for _, v := range g.inits {
		if !emit(v) {
			return
		}
	}
}

func (g graph) Next(v int, emit func(int, int) bool) {
	for i, w := range g.adj[v] {
		if !emit(i, w) {
			return
		}
	}
}

func (graph) AppendKey(buf []byte, v int) []byte { return append(buf, byte(v)) }

// decode turns fuzz bytes into a graph with at most 8 nodes, 3 initial
// emissions, and 3 edges per node, plus a depth limit (-1 or 0..7) and a
// state limit (-1 or 1..11). Missing bytes read as 0.
func decode(data []byte) (g graph, depth, states int) {
	next := func() int {
		if len(data) == 0 {
			return 0
		}
		b := data[0]
		data = data[1:]
		return int(b)
	}
	g.n = 1 + next()%8
	for range next() % 4 {
		g.inits = append(g.inits, next()%g.n)
	}
	g.adj = make([][]int, g.n)
	for v := range g.n {
		for range next() % 4 {
			g.adj[v] = append(g.adj[v], next()%g.n)
		}
	}
	g.bad = uint8(next())
	depth, states = next()%10-1, next()%12
	if depth == 8 {
		depth = -1
	}
	if states == 0 {
		states = -1
	}
	return g, depth, states
}

// encode builds fuzz bytes for decode; limits use decode's ranges.
func encode(n int, inits []int, adj [][]int, bad uint8, depth, states int) []byte {
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

func FuzzExploreMatchesReference(f *testing.F) {
	none := -1
	f.Add(encode(3, []int{0}, [][]int{{1}, {2}, {0}}, 0, none, none))              // cycle
	f.Add(encode(2, []int{0}, [][]int{{0, 1}, {1}}, 0, none, none))                // self-loops
	f.Add(encode(3, []int{0}, [][]int{{1, 1, 2}, {2, 2}, {}}, 0, none, none))      // duplicate edges, dead end
	f.Add(encode(4, []int{2, 0, 2}, [][]int{{1}, {3}, {3}, {}}, 0, none, none))    // multiple + repeated inits
	f.Add(encode(4, []int{0}, [][]int{{1, 2}, {3}, {3}, {}}, 1<<3, none, none))    // violation at depth 2
	f.Add(encode(3, []int{1}, [][]int{{}, {0}, {}}, 1<<1, none, none))             // violating initial state
	f.Add(encode(1, nil, [][]int{{}}, 0, none, none))                              // empty Init
	f.Add(encode(5, []int{0}, [][]int{{1, 2}, {3}, {4}, {4}, {0}}, 1<<4, 1, none)) // depth limit hides violation
	f.Add(encode(5, []int{0, 1}, [][]int{{2, 3}, {4}, {4}, {}, {}}, 0, none, 3))   // state limit
	f.Add(encode(5, []int{0}, [][]int{{1, 2}, {3}, {4}, {}, {}}, 0, 1, 3))         // both limits
	f.Fuzz(func(t *testing.T, data []byte) {
		g, depth, states := decode(data)
		holds := func(v int) bool { return g.bad>>v&1 == 0 }
		cfg := check.Config[int]{Invariants: []check.Invariant[int]{{Name: "good", Holds: holds}}}
		if depth >= 0 {
			cfg.MaxDepth = check.Max(depth)
		}
		if states >= 0 {
			cfg.MaxStates = check.Max(states)
		}
		res, err := check.Run(context.Background(), g, cfg)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		st, ref := res.Stats, reference.Explore(g, holds, depth, states)
		got := []any{res.Status, res.Reason, st.InitEmissions, st.InitAdmitted, st.InitDuplicates, st.Admitted,
			st.Expanded, st.Transitions, st.Duplicates, st.CutoffTransitions, st.StateLimitRefusals, st.RefusalPhase, st.MaxDepth}
		want := []any{refStatus(ref), refReason(ref), int64(ref.InitEmissions), int64(ref.InitAdmitted),
			int64(ref.InitDuplicates), int64(ref.Admitted), int64(ref.Expanded), int64(ref.Transitions),
			int64(ref.Duplicates), int64(ref.Cutoffs), int64(ref.Refusals), refPhase(ref), ref.MaxDepth}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("graph %+v depth %d states %d\n engine    %v\n reference %v", g, depth, states, got, want)
		}
		rInit := int64(0)
		if st.RefusalPhase == check.InitPhase {
			rInit = st.StateLimitRefusals
		}
		if st.InitEmissions != st.InitAdmitted+st.InitDuplicates+rInit ||
			st.Transitions != (st.Admitted-st.InitAdmitted)+st.Duplicates+st.CutoffTransitions+(st.StateLimitRefusals-rInit) {
			t.Fatalf("I1/I2 fail: %+v", st)
		}
		if res.Status == check.Violation {
			if len(res.Violation.Trace.Steps)-1 != ref.ViolationDepth {
				t.Fatalf("trace length %d, minimal depth %d", len(res.Violation.Trace.Steps)-1, ref.ViolationDepth)
			}
			checkTrace(t, g, res.Violation.Trace, holds)
		}
		again, _ := check.Run(context.Background(), g, cfg)
		res.Stats.WallTime, again.Stats.WallTime = 0, 0
		if !reflect.DeepEqual(res, again) {
			t.Fatal("repeated run differs")
		}
	})
}
