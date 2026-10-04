package check_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/models"
)

// With interval K, checks run before dequeues 1, 1+K, 1+2K, ... and never on
// an empty frontier. Water jugs expand 16 states in total.
func TestCheckIntervalSchedule(t *testing.T) {
	for _, c := range []struct {
		k, cancelAfter int
		status         check.Status
		expanded       int64
	}{
		{3, 3, check.Incomplete, 3},   // observed before dequeue 4
		{3, 4, check.Incomplete, 6},   // next check is before dequeue 7
		{5, 15, check.Incomplete, 15}, // observed before dequeue 16
		{5, 16, check.Exhausted, 16},  // frontier empty: no check, normal completion
	} {
		ctx, cancel := context.WithCancel(context.Background())
		m := &cancelAfter[models.Jug, string]{Model: models.Jugs{}, k: c.cancelAfter, cancel: cancel}
		res := run(t, ctx, m, jugCfg{CheckInterval: c.k})
		cancel()
		if res.Status != c.status || res.Stats.Expanded != c.expanded {
			t.Errorf("K=%d cancel after %d: got %v with %d expanded, want %v with %d",
				c.k, c.cancelAfter, res.Status, res.Stats.Expanded, c.status, c.expanded)
		}
	}
}

// deadlineCtx reports DeadlineExceeded once the test says the deadline passed.
type deadlineCtx struct {
	context.Context
	expired bool
}

func (c *deadlineCtx) Err() error {
	if c.expired {
		return context.DeadlineExceeded
	}
	return nil
}

func TestDeadlineMidRun(t *testing.T) {
	ctx := &deadlineCtx{Context: context.Background()}
	calls := 0
	m := hooked[models.Jug, string]{Model: models.Jugs{}, next: func(s models.Jug, emit func(string, models.Jug) bool) {
		models.Jugs{}.Next(s, emit)
		if calls++; calls == 2 {
			ctx.expired = true
		}
	}}
	res := run(t, ctx, m, jugCfg{CheckInterval: 1})
	if res.Status != check.Incomplete || res.Reason != check.DeadlineExceeded || res.Stats.Expanded != 2 {
		t.Errorf("got %v/%v with %d expanded", res.Status, res.Reason, res.Stats.Expanded)
	}
}

func TestMaxHeapBytes(t *testing.T) {
	called := false
	m := hooked[models.Jug, string]{Model: models.Jugs{}, init: func(emit func(models.Jug) bool) {
		called = true
		models.Jugs{}.Init(emit)
	}}
	// The live heap always exceeds one byte, so the check before Init fires.
	res := run(t, context.Background(), m, jugCfg{MaxHeapBytes: 1})
	if res.Status != check.Incomplete || res.Reason != check.MemoryLimit || called {
		t.Errorf("tiny limit: got %v/%v, Init called %v", res.Status, res.Reason, called)
	}
	if res := run(t, context.Background(), models.Jugs{}, jugCfg{MaxHeapBytes: 1 << 50}); res.Status != check.Exhausted {
		t.Errorf("huge limit: got %v", res.Status)
	}
}
