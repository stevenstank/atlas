package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/randgraph"
	"github.com/stevenstank/atlas/internal/reference"
	"github.com/stevenstank/atlas/models"
)

// sameSetsAndDepths checks the part of TESTING.md §8 that the public API
// cannot show: the engine admits exactly the reference explorer's states, at
// the same depths. Limits of -1 mean none. It returns the engine's stats.
func sameSetsAndDepths[S, A any](t *testing.T, m core.Model[S, A], holds func(S) bool, depth, states int) Stats {
	t.Helper()
	var cfg Config[S]
	if holds != nil {
		cfg.Invariants = []Invariant[S]{{Name: "inv", Holds: holds}}
	}
	if depth >= 0 {
		cfg.MaxDepth = Max(depth)
	}
	if states >= 0 {
		cfg.MaxStates = Max(states)
	}
	e, err := newEngine(context.Background(), m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	res := e.result()
	ref := reference.Explore(m, holds, depth, states)
	if e.store.Len() != len(ref.Depths) {
		t.Fatalf("engine admitted %d states, reference %d", e.store.Len(), len(ref.Depths))
	}
	for id := range e.store.Len() {
		k, d := e.store.Key(core.StateID(id)), e.store.Depth(core.StateID(id))
		if rd, ok := ref.Depths[k]; !ok || rd != int(d) {
			t.Fatalf("state %q: engine depth %d, reference depth %d (present %v)", k, d, rd, ok)
		}
	}
	return res.Stats
}

// TestRandomGraphSetsAndDepths runs the same 1,000 seeds as TestRandomGraphs.
func TestRandomGraphSetsAndDepths(t *testing.T) {
	for seed := range uint64(1000) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			g, depth, states := randgraph.Decode(randgraph.Bytes(seed))
			sameSetsAndDepths(t, g, g.Good, depth, states)
		})
	}
}

// TestReferenceModelSetsAndDepths covers every reference model (ROADMAP
// Phase 1), unlimited and under the limits used in CONFORMANCE.md.
func TestReferenceModelSetsAndDepths(t *testing.T) {
	limits := [][2]int{{-1, -1}, {0, -1}, {5, -1}, {-1, 1}, {-1, 12}, {6, 13}}
	for _, l := range limits {
		name := fmt.Sprintf("D%d_N%d", l[0], l[1])
		t.Run("jugs/"+name, func(t *testing.T) { sameSetsAndDepths(t, models.Jugs{}, models.NotFour, l[0], l[1]) })
		t.Run("grid2/"+name, func(t *testing.T) { sameSetsAndDepths(t, models.Origin, nil, l[0], l[1]) })
		t.Run("multi/"+name, func(t *testing.T) { sameSetsAndDepths(t, models.MultiInit, models.Sum, l[0], l[1]) })
		t.Run("gridN/"+name, func(t *testing.T) { sameSetsAndDepths(t, models.GridN{D: 3, K: 4}, nil, l[0], l[1]) })
	}
}

// TestGridNAnalyticCounts checks GridN against its closed-form counts, an
// oracle independent of both the engine and the reference explorer.
func TestGridNAnalyticCounts(t *testing.T) {
	pow := func(b, e int) int64 {
		r := int64(1)
		for range e {
			r *= int64(b)
		}
		return r
	}
	for _, g := range []models.GridN{{D: 1, K: 1}, {D: 1, K: 5}, {D: 2, K: 3}, {D: 3, K: 4}, {D: 4, K: 5}} {
		t.Run(fmt.Sprintf("D%d_K%d", g.D, g.K), func(t *testing.T) {
			st := sameSetsAndDepths(t, g, nil, -1, -1)
			want := Stats{Admitted: pow(g.K, g.D), Expanded: pow(g.K, g.D), MaxDepth: g.D * (g.K - 1),
				Transitions: int64(g.D*(g.K-1)) * pow(g.K, g.D-1), TerminalStates: 1}
			got := Stats{Admitted: st.Admitted, Expanded: st.Expanded, MaxDepth: st.MaxDepth,
				Transitions: st.Transitions, TerminalStates: st.TerminalStates}
			if got != want {
				t.Errorf("got %+v\nwant %+v", got, want)
			}
		})
	}
}
