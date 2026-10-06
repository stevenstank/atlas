package bench_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/bench"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

var (
	grid2Want = bench.Want{Status: check.Exhausted, States: 9, Transitions: 12}
	jugsWant  = bench.Want{Status: check.Violation, States: 13, Transitions: 38, Invariant: "NotFour", TraceLen: 6}
	notFour   = []check.Invariant[models.Jug]{{Name: "NotFour", Holds: models.NotFour}}
)

// Check accepts the real results and rejects each kind of wrong expectation.
func TestCheck(t *testing.T) {
	grid, _ := check.Run(context.Background(), models.Origin, check.Config[models.Cell]{})
	jugs, _ := check.Run(context.Background(), models.Jugs{}, check.Config[models.Jug]{Invariants: notFour})
	if err := bench.Check(grid2Want, grid); err != nil {
		t.Errorf("Grid2: %v", err)
	}
	if err := bench.Check(jugsWant, jugs); err != nil {
		t.Errorf("Jugs: %v", err)
	}
	unchecked := grid2Want
	unchecked.Transitions = -1
	if err := bench.Check(unchecked, grid); err != nil {
		t.Errorf("unchecked transitions: %v", err)
	}
	for name, tc := range map[string]struct {
		w    bench.Want
		jugs bool
		msg  string
	}{
		"states":      {bench.Want{Status: check.Exhausted, States: 10, Transitions: 12}, false, "states discovered (Stats.Admitted) 9, want 10"},
		"transitions": {bench.Want{Status: check.Exhausted, States: 9, Transitions: 11}, false, "transitions 12, want 11"},
		"status":      {bench.Want{Status: check.Bounded, States: 9, Transitions: 12}, false, "status Exhausted, want Bounded"},
		"invariant":   {bench.Want{Status: check.Violation, States: 13, Transitions: 38, Invariant: "X", TraceLen: 6}, true, `invariant "NotFour", want "X"`},
		"trace":       {bench.Want{Status: check.Violation, States: 13, Transitions: 38, Invariant: "NotFour", TraceLen: 5}, true, "trace length 6, want 5"},
		"no trace":    {bench.Want{Status: check.Violation, States: 9, Transitions: 12, Invariant: "NotFour", TraceLen: 6}, false, "no counterexample"},
	} {
		var err error
		if tc.jugs {
			err = bench.Check(tc.w, jugs)
		} else {
			err = bench.Check(tc.w, grid)
		}
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, tc.msg)
		}
	}
}

// A benchmark with a wrong expected state count fails through the same
// runChecked path the real benchmarks use. testing.Benchmark returns a zero
// result (N == 0) for a benchmark that failed.
func TestBenchmarkFailsOnWrongCount(t *testing.T) {
	wrong := grid2Want
	wrong.States = 10
	for name, w := range map[string]bench.Want{"correct": grid2Want, "wrong count": wrong} {
		r := testing.Benchmark(func(b *testing.B) { runChecked(b, models.Origin, check.Config[models.Cell]{}, w) })
		if failed := r.N == 0; failed != (name == "wrong count") {
			t.Errorf("%s: failed = %v (N = %d)", name, failed, r.N)
		}
		if name == "correct" && (r.Extra["states/s"] <= 0 || r.Extra["transitions/s"] <= 0) {
			t.Errorf("metrics missing: %v", r.Extra)
		}
	}
}

// B4's replay check (tracetest.Expect with a *testing.B) fails on a wrong
// expected action.
func TestBenchmarkReplayFailsOnWrongTrace(t *testing.T) {
	inv := notFour[0]
	r := testing.Benchmark(func(b *testing.B) {
		res := runChecked(b, models.Jugs{}, check.Config[models.Jug]{Invariants: notFour}, jugsWant)
		tracetest.Expect(b, models.Jugs{}, inv, res, "FillBig", "BigToSmall", "EmptySmall", "BigToSmall", "FillBig", "FillSmall")
	})
	if r.N != 0 {
		t.Errorf("replay with a wrong action did not fail the benchmark")
	}
}

func TestSuiteTable(t *testing.T) {
	ready := map[string]bool{}
	for _, e := range bench.Suite {
		if e.Ready == (e.Deferred != "") {
			t.Errorf("%s: Ready = %v but Deferred = %q", e.ID, e.Ready, e.Deferred)
		}
		ready[e.ID] = e.Ready
	}
	want := map[string]bool{"B1": true, "B2": true, "B3": false, "B4": true, "B5": false, "B6": false, "B7": false, "B8": false, "V1": false}
	for id, r := range want {
		if got, ok := ready[id]; !ok || got != r {
			t.Errorf("%s: ready = %v (present %v), want %v", id, got, ok, r)
		}
	}
}

func TestCaptureEnv(t *testing.T) {
	env := bench.CaptureEnv()
	for _, kv := range env {
		k, v := kv[0], kv[1]
		if k == "" || strings.ToLower(k) != k || strings.ContainsAny(k, " \t") || v == "" || strings.Contains(v, "\n") {
			t.Errorf("not a benchmark config line: %q: %q", k, v)
		}
	}
	for _, k := range []string{"atlas-suite", "atlas-commit", "atlas-dirty", "go-version", "goos", "goarch",
		"goamd64", "build-flags", "cpu-model", "num-cpu", "gomaxprocs", "gogc", "gomemlimit", "mem-total", "kernel", "wsl"} {
		if env.Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
	if env.Get("atlas-suite") != bench.SuiteVersion {
		t.Errorf("atlas-suite = %q", env.Get("atlas-suite"))
	}
}
