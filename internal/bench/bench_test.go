package bench_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/bench"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
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

// runChecked times check.Run on m and checks every iteration's result
// against want, failing the benchmark on any mismatch (BENCHMARKS.md §4). The
// check is a few integer comparisons; it runs inside the timed loop so that
// no iteration goes unchecked. It reports states/s and transitions/s from
// the engine's own Stats: total admitted states and transitions over total
// Stats.WallTime. It returns the last result for further checks, which are
// not timed.
func runChecked[S, A any](b *testing.B, m core.Model[S, A], cfg check.Config[S], want bench.Want) check.Result[S, A] {
	b.Helper()
	ctx := context.Background()
	var (
		res                 check.Result[S, A]
		states, transitions int64
		wall                time.Duration
	)
	b.ReportAllocs()
	for b.Loop() {
		r, err := check.Run(ctx, m, cfg)
		if err != nil {
			b.Fatal(err)
		}
		if err := bench.Check(want, r); err != nil {
			b.Fatal(err)
		}
		states += r.Stats.Admitted
		transitions += r.Stats.Transitions
		wall += r.Stats.WallTime
		res = r
	}
	if s := wall.Seconds(); s > 0 {
		b.ReportMetric(float64(states)/s, "states/s")
		b.ReportMetric(float64(transitions)/s, "transitions/s")
	}
	return res
}

// B1: Grid2, 9 states and 12 transitions (CONFORMANCE.md G3/M1, SEMANTICS.md §6).
func BenchmarkB1Grid2(b *testing.B) {
	runChecked(b, models.Origin, check.Config[models.Cell]{},
		bench.Want{Status: check.Exhausted, States: 9, Transitions: 12})
}

// B2: GridN with D=4. Expected counts are analytic (models.GridN):
// K^D states and D*(K-1)*K^(D-1) transitions.
func BenchmarkB2GridN(b *testing.B) {
	for _, k := range []int{10, 20, 40} {
		g := models.GridN{D: 4, K: k}
		kd := int64(k * k * k)
		want := bench.Want{Status: check.Exhausted, States: kd * int64(k), Transitions: 4 * int64(k-1) * kd}
		b.Run(fmt.Sprintf("K=%d", k), func(b *testing.B) {
			runChecked(b, g, check.Config[[]int]{}, want)
		})
	}
}

// B4: water jugs with NotFour, CONFORMANCE.md row J2: Violation after 13
// states and 38 transitions, with the 6-step counterexample replayed after
// timing.
func BenchmarkB4Jugs(b *testing.B) {
	inv := check.Invariant[models.Jug]{Name: "NotFour", Holds: models.NotFour}
	res := runChecked(b, models.Jugs{}, check.Config[models.Jug]{Invariants: []check.Invariant[models.Jug]{inv}},
		bench.Want{Status: check.Violation, States: 13, Transitions: 38, Invariant: "NotFour", TraceLen: 6})
	tracetest.Expect(b, models.Jugs{}, inv, res,
		"FillBig", "BigToSmall", "EmptySmall", "BigToSmall", "FillBig", "BigToSmall")
}
