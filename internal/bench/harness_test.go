package bench_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/bench"
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

func mustFind(t testing.TB, name string) bench.Workload {
	t.Helper()
	w, err := bench.Find(name)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A benchmark with a wrong expected state count fails through the same
// runChecked path the real benchmarks use. testing.Benchmark returns a zero
// result (N == 0) for a benchmark that failed.
func TestBenchmarkFailsOnWrongCount(t *testing.T) {
	good := mustFind(t, "B1")
	wrong := good
	wrong.Want.States = 10
	for name, w := range map[string]bench.Workload{"correct": good, "wrong count": wrong} {
		r := testing.Benchmark(func(b *testing.B) { runChecked(b, w) })
		if failed := r.N == 0; failed != (name == "wrong count") {
			t.Errorf("%s: failed = %v (N = %d)", name, failed, r.N)
		}
		if name == "correct" && (r.Extra["states/s"] <= 0 || r.Extra["transitions/s"] <= 0) {
			t.Errorf("metrics missing: %v", r.Extra)
		}
	}
}

// B4's replay, run by runChecked after timing, fails on a wrong expected
// action.
func TestBenchmarkReplayFailsOnWrongTrace(t *testing.T) {
	w := mustFind(t, "B4")
	w.Want.Trace = append(append([]string{}, w.Want.Trace[:5]...), "FillSmall")
	if r := testing.Benchmark(func(b *testing.B) { runChecked(b, w) }); r.N != 0 {
		t.Errorf("replay with a wrong action did not fail the benchmark")
	}
}

// Every ready suite entry has workloads, names are unique, and Find rejects
// what it cannot resolve without guessing.
func TestWorkloads(t *testing.T) {
	names := map[string]bool{}
	ids := map[string]int{}
	for _, w := range bench.Workloads() {
		if names[w.Name()] {
			t.Errorf("duplicate workload %s", w.Name())
		}
		names[w.Name()] = true
		ids[w.ID]++
	}
	for _, e := range bench.Suite {
		if e.Ready != (ids[e.ID] > 0) {
			t.Errorf("%s: ready = %v but %d workloads", e.ID, e.Ready, ids[e.ID])
		}
	}
	for id, n := range map[string]int{"B1": 1, "B2": 3, "B3": 1, "B4": 1, "B5": 4, "B6": 3, "B7": 1, "B8": 6, "V1": 1} {
		if ids[id] != n {
			t.Errorf("%s: %d workloads, want %d", id, ids[id], n)
		}
	}
	if w := mustFind(t, "B8/pad=256/k=40"); w.Param("d") != 4 || w.Param("k") != 40 || w.Param("pad") != 256 || w.Param("x") != -1 {
		t.Errorf("B8 params: %v", w.Params())
	}
	for name, msg := range map[string]string{
		"B2": "needs parameters", "B8/pad=64": "needs parameters", "B2/k=11": "unknown workload",
		"B9": "unknown workload", "": "unknown workload", "b1": "unknown workload",
	} {
		if _, err := bench.Find(name); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("Find(%q) = %v, want %q", name, err, msg)
		}
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
	want := map[string]bool{"B1": true, "B2": true, "B3": true, "B4": true, "B5": true, "B6": true, "B7": true, "B8": true, "V1": true}
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
