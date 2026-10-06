package bench_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stevenstank/atlas/internal/bench"
)

// TestMain prints the environment as benchmark configuration lines when
// benchmarks are requested, so it is recorded with every result set.
func TestMain(m *testing.M) {
	flag.Parse()
	if f := flag.Lookup("test.bench"); f != nil && f.Value.String() != "" {
		fmt.Print(bench.CaptureEnv())
	}
	os.Exit(m.Run())
}

// runChecked times w.Run and checks every iteration's result against w.Want,
// failing the benchmark on any mismatch (BENCHMARKS.md §4). The check is a
// few integer comparisons; it runs inside the timed loop so that no
// iteration goes unchecked. It reports states/s and transitions/s from the
// engine's own Stats: total admitted states and transitions over total
// Stats.WallTime. The counterexample replay, when Want.Trace is set, runs
// once after timing.
func runChecked(b *testing.B, w bench.Workload) {
	b.Helper()
	ctx := context.Background()
	var (
		last                bench.Outcome
		states, transitions int64
		wall                time.Duration
	)
	b.ReportAllocs()
	for b.Loop() {
		o := w.Run(ctx, nil)
		if err := w.Want.Check(o.Summary); err != nil {
			b.Fatal(err)
		}
		states += o.Stats.Admitted
		transitions += o.Stats.Transitions
		wall += o.Stats.WallTime
		last = o
	}
	if s := wall.Seconds(); s > 0 {
		b.ReportMetric(float64(states)/s, "states/s")
		b.ReportMetric(float64(transitions)/s, "transitions/s")
	}
	if last.Replay != nil {
		if err := last.Replay(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSuite runs every ready workload of BENCHMARKS.md §3 as a
// sub-benchmark named by Workload.Name, e.g. BenchmarkSuite/B2/k=10.
func BenchmarkSuite(b *testing.B) {
	for _, w := range bench.Workloads() {
		b.Run(w.Name(), func(b *testing.B) { runChecked(b, w) })
	}
}
