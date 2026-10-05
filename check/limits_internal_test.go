package check

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/atlas/models"
)

// TestNoStopReasonIsExhausted: every StopReason has a run that produces it,
// and none of those runs is Exhausted (ROADMAP Phase 1 exit criterion,
// TESTING.md §7). Adding a StopReason without a case here fails the test.
func TestNoStopReasonIsExhausted(t *testing.T) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()
	expired, cancel2 := context.WithDeadline(bg, time.Unix(0, 0))
	defer cancel2()
	runJugs := func(ctx context.Context, cfg Config[models.Jug]) Result[models.Jug, string] {
		res, err := Run(ctx, models.Jugs{}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	capacity := func() Result[models.Jug, string] { // a real run would need 2^32 states
		e, _ := newEngine(bg, models.Jugs{}, Config[models.Jug]{})
		e.stopCapacity(&guard{active: true})
		return *e.end
	}
	cases := map[StopReason]func() Result[models.Jug, string]{
		DepthLimit:       func() Result[models.Jug, string] { return runJugs(bg, Config[models.Jug]{MaxDepth: Max(5)}) },
		StateLimit:       func() Result[models.Jug, string] { return runJugs(bg, Config[models.Jug]{MaxStates: Max(15)}) },
		Canceled:         func() Result[models.Jug, string] { return runJugs(canceled, Config[models.Jug]{}) },
		DeadlineExceeded: func() Result[models.Jug, string] { return runJugs(expired, Config[models.Jug]{}) },
		MemoryLimit:      func() Result[models.Jug, string] { return runJugs(bg, Config[models.Jug]{MaxHeapBytes: 1}) },
		CapacityLimit:    capacity,
	}
	for r := NoReason + 1; r <= CapacityLimit; r++ {
		f, ok := cases[r]
		if !ok {
			t.Errorf("no test case for %v", r)
			continue
		}
		res := f()
		if res.Reason != r || res.Status == Exhausted {
			t.Errorf("%v: got %v/%v", r, res.Status, res.Reason)
		}
		// D-013: the printed form must not read as success either.
		if first, _, _ := strings.Cut(res.String(), "\n"); !strings.Contains(first, "NOT VERIFIED ("+r.String()+")") {
			t.Errorf("%v: printed as %q", r, first)
		}
	}
}

func TestStringers(t *testing.T) {
	for v, want := range map[fmt.Stringer]string{
		Exhausted: "Exhausted", Bounded: "Bounded", Incomplete: "Incomplete", Violation: "Violation",
		ModelError: "ModelError", Status(99): "Status(99)",
		NoReason: "none", DepthLimit: "DepthLimit", StateLimit: "StateLimit", Canceled: "Canceled",
		DeadlineExceeded: "DeadlineExceeded", MemoryLimit: "MemoryLimit", CapacityLimit: "CapacityLimit",
		StopReason(99): "StopReason(99)",
	} {
		if got := v.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}
