package bench

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

// Workload is one benchmark case: a suite entry with fixed parameters and its
// expected result. The testing.B suite and cmd/atlasbench both run workloads
// from this one table, so they share one oracle.
type Workload struct {
	ID    string
	Vary  [][2]string // parameters that select the case; part of Name
	Fixed [][2]string // parameters fixed by the suite
	Want  Want
	run   func(ctx context.Context, onViolation func()) (Summary, check.Stats, func([]string) error)
}

// Name is the ID followed by the varying parameters, e.g. "B2/k=10".
func (w Workload) Name() string {
	parts := []string{w.ID}
	for _, kv := range w.Vary {
		parts = append(parts, kv[0]+"="+kv[1])
	}
	return strings.Join(parts, "/")
}

// Params returns every parameter, varying first.
func (w Workload) Params() [][2]string { return append(append([][2]string{}, w.Vary...), w.Fixed...) }

// Param returns the integer parameter key, or -1.
func (w Workload) Param(key string) int {
	for _, kv := range w.Params() {
		if kv[0] == key {
			n, _ := strconv.Atoi(kv[1])
			return n
		}
	}
	return -1
}

// Outcome is one run of a workload.
type Outcome struct {
	Summary
	Stats check.Stats
	// Replay checks the counterexample against Want.Trace without the engine
	// (tracetest.Replay); nil when Want.Trace is nil.
	Replay func() error
}

// Run calls check.Run once on the workload. If onViolation is not nil, it is
// called once, from inside the invariant evaluation, the first time an
// invariant returns false: that is when the engine first admits the
// violating state, before it rebuilds the trace. With a nil onViolation the
// invariants are passed to check.Run unwrapped.
func (w Workload) Run(ctx context.Context, onViolation func()) Outcome {
	sum, stats, replay := w.run(ctx, onViolation)
	o := Outcome{Summary: sum, Stats: stats}
	if w.Want.Trace != nil {
		o.Replay = func() error { return replay(w.Want.Trace) }
	}
	return o
}

func workload[S, A any](id string, vary, fixed [][2]string, m core.Model[S, A], invs []check.Invariant[S], want Want) Workload {
	w := Workload{ID: id, Vary: vary, Fixed: fixed, Want: want}
	w.run = func(ctx context.Context, hook func()) (Summary, check.Stats, func([]string) error) {
		cfg := check.Config[S]{Invariants: invs}
		if hook != nil {
			fired := false
			cfg.Invariants = make([]check.Invariant[S], len(invs))
			for i, inv := range invs {
				cfg.Invariants[i] = check.Invariant[S]{Name: inv.Name, Holds: func(s S) bool {
					ok := inv.Holds(s)
					if !ok && !fired {
						fired = true
						hook()
					}
					return ok
				}}
			}
		}
		res, err := check.Run(ctx, m, cfg)
		if err != nil {
			panic(fmt.Sprintf("bench: %s: invalid configuration: %v", id, err)) // a bug in this table
		}
		replay := func(trace []string) error {
			for _, inv := range invs {
				if inv.Name == want.Invariant {
					return tracetest.Replay(m, inv, res, trace...)
				}
			}
			return fmt.Errorf("bench: %s: no invariant %q", id, want.Invariant)
		}
		return Summarize(res), res.Stats, replay
	}
	return w
}

func kv(k string, v int) [2]string { return [2]string{k, strconv.Itoa(v)} }

// gridWant is GridN's analytic result (models.GridN): K^D states and
// D*(K-1)*K^(D-1) transitions.
func gridWant(d, k int) Want {
	states, trans := int64(1), int64(d*(k-1))
	for range d {
		states *= int64(k)
	}
	for range d - 1 {
		trans *= int64(k)
	}
	return Want{Status: check.Exhausted, States: states, Transitions: trans}
}

// Workloads returns every ready workload in suite order. Expected values and
// their provenance:
//   - B1: CONFORMANCE.md G3/M1 and SEMANTICS.md §6.
//   - B2, B8: GridN's analytic counts; B8's payload changes no count
//     (TestPaddedGridMatchesGridN).
//   - B3: 9!/2 states (TESTING.md §1) and 20,160 * 24 transitions
//     (TestPuzzle8Counts).
//   - B4: CONFORMANCE.md row J2 and its trace.
//   - B5: N=1 by hand; N=3..6 from the reference explorer (BENCHMARKS.md §3)
//     before any measurement. BENCHMARKS.md says "count from Phase 2 record",
//     but Phase 2 built no two-phase commit model: there is no such record.
//   - B6: caps 2 and 3 equal the Phase 2 records (CONFORMANCE.md, 3×2 and
//     3×3); cap 4 from the reference explorer before any measurement.
//   - B7: the reference explorer, agreeing with the engine (TestB7Oracle).
//   - V1: the Phase 2 record for Register 3×2.
func Workloads() []Workload {
	var ws []Workload
	ws = append(ws, workload("B1", nil, nil, models.Origin, nil,
		Want{Status: check.Exhausted, States: 9, Transitions: 12}))
	for _, k := range []int{10, 20, 40} {
		ws = append(ws, workload("B2", [][2]string{kv("k", k)}, [][2]string{kv("d", 4)},
			models.GridN{D: 4, K: k}, nil, gridWant(4, k)))
	}
	ws = append(ws, workload("B3", nil, nil, Puzzle8{}, nil,
		Want{Status: check.Exhausted, States: 181440, Transitions: 483840}))
	ws = append(ws, workload("B4", nil, [][2]string{kv("big", 5), kv("small", 3)}, models.Jugs{},
		[]check.Invariant[models.Jug]{{Name: "NotFour", Holds: models.NotFour}},
		Want{Status: check.Violation, States: 13, Transitions: 38, Invariant: "NotFour", TraceLen: 6,
			Trace: []string{"FillBig", "BigToSmall", "EmptySmall", "BigToSmall", "FillBig", "BigToSmall"}}))
	for _, c := range []struct {
		n                   int
		states, transitions int64
	}{{3, 288, 761}, {4, 1568, 5377}, {5, 8832, 37345}, {6, 50816, 256001}} {
		ws = append(ws, workload("B5", [][2]string{kv("n", c.n)}, nil, TwoPhase{N: c.n},
			[]check.Invariant[TPState]{{Name: "TCConsistent", Holds: TCConsistent}},
			Want{Status: check.Exhausted, States: c.states, Transitions: c.transitions}))
	}
	abpInvs := []check.Invariant[models.ABPState]{
		{Name: "InOrderDelivery", Holds: models.InOrderDelivery},
		{Name: "AckedDelivered", Holds: models.AckedDelivered},
	}
	for _, c := range []struct {
		cap                 int
		states, transitions int64
	}{{2, 81, 299}, {3, 172, 775}, {4, 315, 1577}} {
		ws = append(ws, workload("B6", [][2]string{kv("cap", c.cap)}, [][2]string{kv("msgs", 3)},
			models.ABP{Msgs: 3, Cap: c.cap}, abpInvs, Want{Status: check.Exhausted, States: c.states, Transitions: c.transitions}))
	}
	ws = append(ws, workload("B7", nil, [][2]string{kv("tasks", 8), kv("workers", 2), kv("crashes", 1)},
		EarlyClose{TaskQueue: models.TaskQueue{Tasks: 8, Workers: 2, Crashes: 1}},
		[]check.Invariant[ECState]{{Name: "NoLostTask", Holds: ECNoLostTask}},
		Want{Status: check.Violation, States: 166581, Transitions: 486921, Invariant: "NoLostTask", TraceLen: 21, Trace: b7Trace()}))
	for _, pad := range []int{64, 256} {
		for _, k := range []int{10, 20, 40} {
			ws = append(ws, workload("B8", [][2]string{kv("pad", pad), kv("k", k)}, [][2]string{kv("d", 4)},
				PaddedGrid{D: 4, K: k, Pad: pad}, nil, gridWant(4, k)))
		}
	}
	// V1, held out: the correct concurrent register at its largest Phase 2
	// size, with the Phase 2 record (models/register_test.go,
	// TestRegisterCorrect, cross-checked by the reference explorer).
	ws = append(ws, workload("V1", nil, [][2]string{kv("clients", 3), kv("ops", 2)}, models.Register{Clients: 3, Ops: 2},
		[]check.Invariant[models.RegState]{{Name: "NoStaleRead", Holds: models.NoStaleRead}},
		Want{Status: check.Exhausted, States: 25543, Transitions: 58698}))
	return ws
}

// b7Trace is B7's shortest counterexample: A carries t0..t5 through take,
// run, and ack; A and B take the last two tasks, which closes the queue; A
// crashes and t6 is dropped.
func b7Trace() []string {
	var tr []string
	for i := range 6 {
		tr = append(tr, fmt.Sprintf("A takes t%d", i), fmt.Sprintf("A runs t%d: effect applied (run 1)", i), fmt.Sprintf("A acks t%d: done", i))
	}
	return append(tr, "A takes t6", "B takes t7",
		"A crashes holding t6; the queue is closed (every task delivered), so t6 is dropped")
}

// Find returns the workload with the given Name. It never guesses: a suite
// ID whose cases need parameters, or an unknown name, is an error that lists
// the valid names.
func Find(name string) (Workload, error) {
	var ids []string
	for _, w := range Workloads() {
		if w.Name() == name {
			return w, nil
		}
		if w.ID == name || strings.HasPrefix(w.Name(), name+"/") {
			ids = append(ids, w.Name())
		}
	}
	if ids != nil {
		return Workload{}, fmt.Errorf("bench: %s needs parameters; one of: %s", name, strings.Join(ids, ", "))
	}
	return Workload{}, fmt.Errorf("bench: unknown workload %q (use -list)", name)
}
