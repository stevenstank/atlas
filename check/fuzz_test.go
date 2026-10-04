package check_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/randgraph"
	"github.com/stevenstank/atlas/internal/reference"
)

func FuzzExploreMatchesReference(f *testing.F) {
	none := -1
	f.Add(randgraph.Encode(3, []int{0}, [][]int{{1}, {2}, {0}}, 0, none, none))              // cycle
	f.Add(randgraph.Encode(2, []int{0}, [][]int{{0, 1}, {1}}, 0, none, none))                // self-loops
	f.Add(randgraph.Encode(3, []int{0}, [][]int{{1, 1, 2}, {2, 2}, {}}, 0, none, none))      // duplicate edges, dead end
	f.Add(randgraph.Encode(4, []int{2, 0, 2}, [][]int{{1}, {3}, {3}, {}}, 0, none, none))    // multiple + repeated inits
	f.Add(randgraph.Encode(4, []int{0}, [][]int{{1, 2}, {3}, {3}, {}}, 1<<3, none, none))    // violation at depth 2
	f.Add(randgraph.Encode(3, []int{1}, [][]int{{}, {0}, {}}, 1<<1, none, none))             // violating initial state
	f.Add(randgraph.Encode(1, nil, [][]int{{}}, 0, none, none))                              // empty Init
	f.Add(randgraph.Encode(5, []int{0}, [][]int{{1, 2}, {3}, {4}, {4}, {0}}, 1<<4, 1, none)) // depth limit hides violation
	f.Add(randgraph.Encode(5, []int{0, 1}, [][]int{{2, 3}, {4}, {4}, {}, {}}, 0, none, 3))   // state limit
	f.Add(randgraph.Encode(5, []int{0}, [][]int{{1, 2}, {3}, {4}, {}, {}}, 0, 1, 3))         // both limits
	f.Fuzz(checkGraph)
}

// TestRandomGraphs is the property test of TESTING.md §8: 1,000 seeded random
// graphs through the same checks as the fuzz target. Reproduce a failure with
// go test ./check -run 'TestRandomGraphs/seed<N>$'.
func TestRandomGraphs(t *testing.T) {
	for seed := range uint64(1000) {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) { checkGraph(t, randgraph.Bytes(seed)) })
	}
}

// checkGraph compares the engine with the reference explorer on one graph:
// outcome, reason, every count, I1/I2, trace minimality and replay, and
// determinism.
func checkGraph(t *testing.T, data []byte) {
	g, depth, states := randgraph.Decode(data)
	holds := g.Good
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
}
