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

// B3: the 8-puzzle, 181,440 states (9!/2, TESTING.md §1) and 483,840
// transitions (TestPuzzle8Counts derives both).
func BenchmarkB3Puzzle8(b *testing.B) {
	runChecked(b, bench.Puzzle8{}, check.Config[bench.Board]{},
		bench.Want{Status: check.Exhausted, States: 181440, Transitions: 483840})
}

// b5Counts are the TwoPhase counts with TCConsistent checked, recorded from
// the reference explorer (BENCHMARKS.md §3) before any measurement;
// TestTwoPhaseCounts re-checks them against both explorers. BENCHMARKS.md
// says "count from Phase 2 record", but Phase 2 built no two-phase commit
// model: there is no such record.
var b5Counts = []struct {
	n                   int
	states, transitions int64
}{{3, 288, 761}, {4, 1568, 5377}, {5, 8832, 37345}, {6, 50816, 256001}}

var tcConsistent = []check.Invariant[bench.TPState]{{Name: "TCConsistent", Holds: bench.TCConsistent}}

// B5: two-phase commit with N = 3..6 resource managers.
func BenchmarkB5TwoPhase(b *testing.B) {
	for _, c := range b5Counts {
		b.Run(fmt.Sprintf("N=%d", c.n), func(b *testing.B) {
			runChecked(b, bench.TwoPhase{N: c.n}, check.Config[bench.TPState]{Invariants: tcConsistent},
				bench.Want{Status: check.Exhausted, States: c.states, Transitions: c.transitions})
		})
	}
}

// b6Counts are models.ABP with 3 messages and both Phase 2 invariants. Caps 2
// and 3 equal the Phase 2 records (docs/CONFORMANCE.md, 3×2 and 3×3); cap 4
// was recorded from the reference explorer before any measurement.
// TestABPBenchCounts re-checks all three against both explorers.
var b6Counts = []struct {
	cap                 int
	states, transitions int64
}{{2, 81, 299}, {3, 172, 775}, {4, 315, 1577}}

var abpInvariants = []check.Invariant[models.ABPState]{
	{Name: "InOrderDelivery", Holds: models.InOrderDelivery},
	{Name: "AckedDelivered", Holds: models.AckedDelivered},
}

// B6: bounded lossy message passing; see the B6 entry in bench.Suite.
func BenchmarkB6ABP(b *testing.B) {
	for _, c := range b6Counts {
		b.Run(fmt.Sprintf("cap=%d", c.cap), func(b *testing.B) {
			runChecked(b, models.ABP{Msgs: 3, Cap: c.cap}, check.Config[models.ABPState]{Invariants: abpInvariants},
				bench.Want{Status: check.Exhausted, States: c.states, Transitions: c.transitions})
		})
	}
}

// B8: PaddedGrid at B2's sizes. The payload changes no count, so the
// expected values are GridN's analytic ones (TestPaddedGridMatchesGridN).
func BenchmarkB8PaddedGrid(b *testing.B) {
	for _, pad := range []int{64, 256} {
		for _, k := range []int{10, 20, 40} {
			kd := int64(k * k * k)
			want := bench.Want{Status: check.Exhausted, States: kd * int64(k), Transitions: 4 * int64(k-1) * kd}
			b.Run(fmt.Sprintf("pad=%d/K=%d", pad, k), func(b *testing.B) {
				runChecked(b, bench.PaddedGrid{D: 4, K: k, Pad: pad}, check.Config[[]byte]{}, want)
			})
		}
	}
}
