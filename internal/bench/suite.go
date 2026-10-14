// Package bench is the Phase 3 benchmark harness (docs/BENCHMARKS.md). It
// holds the suite table, the result check every benchmark must pass, and
// environment capture. The benchmarks themselves are in bench_test.go.
package bench

import (
	"fmt"

	"github.com/stevenstank/atlas/check"
)

// SuiteVersion identifies the suite (BENCHMARKS.md §3). Version 1 is the
// first complete suite, B1–B8 and V1, and the Phase 3 baseline is measured
// under it. Changing any workload or expected result needs a new version.
const SuiteVersion = "1"

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
	// V1 is held out: it is measured only to confirm that a gain is real,
	// never to guide optimization (BENCHMARKS.md §3).
	{ID: "V1", Model: "held-out validation: concurrent register (models.Register)", Params: "3 clients, 2 ops", Ready: true},
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
	// Trace is the expected action sequence, replayed after timing
	// (tracetest.Replay); nil means no replay.
	Trace []string
}

// Summary is what Want checks, taken from a check.Result.
type Summary struct {
	Status              check.Status
	States, Transitions int64
	Invariant           string // "" unless Status is Violation
	TraceLen            int    // -1 unless Status is Violation
}

// Summarize extracts the Summary of res.
func Summarize[S, A any](res check.Result[S, A]) Summary {
	s := Summary{Status: res.Status, States: res.Stats.Admitted, Transitions: res.Stats.Transitions, TraceLen: -1}
	if v := res.Violation; v != nil {
		s.Invariant, s.TraceLen = v.Invariant, len(v.Trace.Steps)-1
	}
	return s
}

// Check returns an error describing every way res differs from w.
func Check[S, A any](w Want, res check.Result[S, A]) error { return w.Check(Summarize(res)) }

// Check returns an error describing every way s differs from w.
func (w Want) Check(s Summary) error {
	var errs []string
	if s.Status != w.Status {
		errs = append(errs, fmt.Sprintf("status %v, want %v", s.Status, w.Status))
	}
	if s.States != w.States {
		errs = append(errs, fmt.Sprintf("states discovered (Stats.Admitted) %d, want %d", s.States, w.States))
	}
	if w.Transitions >= 0 && s.Transitions != w.Transitions {
		errs = append(errs, fmt.Sprintf("transitions %d, want %d", s.Transitions, w.Transitions))
	}
	if w.Status == check.Violation {
		switch {
		case s.TraceLen < 0:
			errs = append(errs, "no counterexample")
		case s.Invariant != w.Invariant:
			errs = append(errs, fmt.Sprintf("invariant %q, want %q", s.Invariant, w.Invariant))
		case s.TraceLen != w.TraceLen:
			errs = append(errs, fmt.Sprintf("trace length %d, want %d", s.TraceLen, w.TraceLen))
		}
	}
	if errs == nil {
		return nil
	}
	return fmt.Errorf("bench: wrong result: %v", errs)
}
