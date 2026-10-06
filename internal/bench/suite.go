// Package bench is the Phase 3 benchmark harness (docs/BENCHMARKS.md). It
// holds the suite table, the result check every benchmark must pass, and
// environment capture. The benchmarks themselves are in bench_test.go.
package bench

import (
	"fmt"

	"github.com/stevenstank/atlas/check"
)

// SuiteVersion identifies the suite (BENCHMARKS.md §3). It is provisional:
// only part of the suite exists, so no result recorded under it is a Phase 3
// baseline. Version 1 is assigned when the full suite exists.
const SuiteVersion = "0-partial"

// Entry is one row of the BENCHMARKS.md §3 suite table.
type Entry struct {
	ID       string
	Model    string
	Params   string
	Ready    bool   // a checked benchmark exists for it
	Deferred string // why not, when !Ready
}

// Suite lists every suite entry and whether this harness implements it.
// Deferred entries wait on owner decisions; no substitutes are used.
var Suite = []Entry{
	{ID: "B1", Model: "Grid2", Params: "fixed", Ready: true},
	{ID: "B2", Model: "GridN", Params: "D=4, K in {10,20,40}", Ready: true},
	{ID: "B3", Model: "8-puzzle (Puzzle8)", Params: "fixed", Ready: true},
	{ID: "B4", Model: "Water jugs", Params: "big 5 L, small 3 L", Ready: true},
	{ID: "B5", Model: "Two-phase commit (TwoPhase)", Params: "N RMs in {3,4,5,6}", Ready: true},
	// B6 uses the existing bounded ABP model as the repository's bounded
	// message-passing workload. Capacity varies across 2, 3, and 4; the
	// model representation itself is fixed-size.
	{ID: "B6", Model: "Message passing (models.ABP)", Params: "3 messages, channel cap in {2,3,4}", Ready: true},
	{ID: "B7", Model: "Task queue, broken variant (EarlyClose)", Params: "8 tasks, 2 workers, 1 crash", Ready: true},
	{ID: "B8", Model: "GridN + padded payload (PaddedGrid)", Params: "D=4, K in {10,20,40}, payload in {64,256} B", Ready: true},
	{ID: "V1", Model: "held-out validation model", Params: "chosen in Phase 3", Deferred: "not chosen yet"},
}

// Want is a benchmark's expected semantic result (BENCHMARKS.md §3–4).
type Want struct {
	Status check.Status
	// States is BENCHMARKS.md's "states discovered", which is
	// check.Stats.Admitted.
	States int64
	// Transitions is check.Stats.Transitions; negative means not checked.
	Transitions int64
	// Invariant and TraceLen (steps after the initial state) are checked
	// when Status is Violation.
	Invariant string
	TraceLen  int
}

// Check returns an error describing every way res differs from w.
func Check[S, A any](w Want, res check.Result[S, A]) error {
	var errs []string
	if res.Status != w.Status {
		errs = append(errs, fmt.Sprintf("status %v, want %v", res.Status, w.Status))
	}
	if res.Stats.Admitted != w.States {
		errs = append(errs, fmt.Sprintf("states discovered (Stats.Admitted) %d, want %d", res.Stats.Admitted, w.States))
	}
	if w.Transitions >= 0 && res.Stats.Transitions != w.Transitions {
		errs = append(errs, fmt.Sprintf("transitions %d, want %d", res.Stats.Transitions, w.Transitions))
	}
	if w.Status == check.Violation {
		switch v := res.Violation; {
		case v == nil:
			errs = append(errs, "no counterexample")
		case v.Invariant != w.Invariant:
			errs = append(errs, fmt.Sprintf("invariant %q, want %q", v.Invariant, w.Invariant))
		case len(v.Trace.Steps)-1 != w.TraceLen:
			errs = append(errs, fmt.Sprintf("trace length %d, want %d", len(v.Trace.Steps)-1, w.TraceLen))
		}
	}
	if errs == nil {
		return nil
	}
	return fmt.Errorf("bench: wrong result: %v", errs)
}
