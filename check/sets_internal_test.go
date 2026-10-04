package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/randgraph"
	"github.com/stevenstank/atlas/internal/reference"
)

// TestRandomGraphSetsAndDepths checks the part of TESTING.md §8 that the
// public API cannot show: on the same 1,000 seeds as TestRandomGraphs, the
// engine admits exactly the reference explorer's states, at the same depths.
func TestRandomGraphSetsAndDepths(t *testing.T) {
	for seed := range uint64(1000) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			g, depth, states := randgraph.Decode(randgraph.Bytes(seed))
			cfg := Config[int]{Invariants: []Invariant[int]{{Name: "good", Holds: g.Good}}}
			if depth >= 0 {
				cfg.MaxDepth = Max(depth)
			}
			if states >= 0 {
				cfg.MaxStates = Max(states)
			}
			e, err := newEngine(context.Background(), g, cfg)
			if err != nil {
				t.Fatal(err)
			}
			e.result()
			ref := reference.Explore(g, g.Good, depth, states)
			if e.store.Len() != len(ref.Depths) {
				t.Fatalf("engine admitted %d states, reference %d", e.store.Len(), len(ref.Depths))
			}
			for id := range e.store.Len() {
				k, d := e.store.Key(core.StateID(id)), e.store.Depth(core.StateID(id))
				if rd, ok := ref.Depths[k]; !ok || rd != int(d) {
					t.Fatalf("state %q: engine depth %d, reference depth %d (present %v)", k, d, rd, ok)
				}
			}
		})
	}
}
