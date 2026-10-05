package models_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/reference"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

var noStale = check.Invariant[models.RegState]{Name: "NoStaleRead", Holds: models.NoStaleRead}

func runRegister(t *testing.T, m models.Register) check.Result[models.RegState, models.RegAction] {
	t.Helper()
	res, err := check.Run(context.Background(), m, check.Config[models.RegState]{
		Invariants: []check.Invariant[models.RegState]{noStale}, DetectMutation: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// The correct register is Exhausted at every size. The 2-client, 1-op counts
// were derived by hand (docs/CONFORMANCE.md, "Concurrent register"); every
// row is also checked against the independent reference explorer below.
// Terminal states are the states where every client has finished, so the
// depth is always 2*Clients*Ops.
func TestRegisterCorrect(t *testing.T) {
	for _, tc := range []struct {
		clients, ops                         int
		states, transitions, terminal, depth int64
	}{
		{2, 1, 34, 44, 9, 4},
		{2, 2, 449, 708, 61, 8},
		{3, 1, 325, 534, 49, 6},
		{2, 3, 3674, 6444, 309, 12},
		{3, 2, 25543, 58698, 901, 12},
	} {
		m := models.Register{Clients: tc.clients, Ops: tc.ops}
		res := runRegister(t, m)
		s := res.Stats
		if res.Status != check.Exhausted || s.Admitted != tc.states || s.Transitions != tc.transitions ||
			s.TerminalStates != tc.terminal || int64(s.MaxDepth) != tc.depth {
			t.Errorf("%+v: %v", m, res)
		}
		ref := reference.Explore(m, models.NoStaleRead, -1, -1)
		if ref.Status != "Exhausted" || int64(ref.Admitted) != tc.states || int64(ref.Edges) != tc.transitions {
			t.Errorf("%+v: reference explorer: %s, %d states, %d edges", m, ref.Status, ref.Admitted, ref.Edges)
		}
	}
}

// TestRegression_StaleReadWithoutInvalidation pins the broken register's
// shortest counterexample: after A's write finishes, B still holds the old
// value in its cache and returns it. The stop-time counts are cross-checked
// with the reference explorer.
func TestRegression_StaleReadWithoutInvalidation(t *testing.T) {
	for _, tc := range []struct {
		clients, ops int
		states       int64
	}{{2, 1, 28}, {2, 2, 52}, {3, 2, 130}} {
		m := models.Register{Clients: tc.clients, Ops: tc.ops, NoInvalidate: true}
		res := runRegister(t, m)
		tracetest.Expect(t, m, noStale, res,
			"A starts write(1)",
			"A write(1) done: memory = 1",
			"B starts read",
			"B read returns 0 (cache hit)")
		ref := reference.Explore(m, models.NoStaleRead, -1, -1)
		if res.Stats.Admitted != tc.states || ref.Status != "Violation" || int64(ref.Admitted) != tc.states || ref.ViolationDepth != 4 {
			t.Errorf("%+v: %d states; reference %s, %d states, depth %d", m, res.Stats.Admitted, ref.Status, ref.Admitted, ref.ViolationDepth)
		}
	}
}

func TestRegisterDeterministic(t *testing.T) {
	for _, bug := range []bool{false, true} {
		m := models.Register{Clients: 2, Ops: 2, NoInvalidate: bug}
		first := runRegister(t, m)
		first.Stats.WallTime = 0
		for range 5 {
			res := runRegister(t, m)
			res.Stats.WallTime = 0
			if res.String() != first.String() || res.Stats != first.Stats {
				t.Fatalf("NoInvalidate=%v: run differs:\n%v\nvs\n%v", bug, res, first)
			}
		}
	}
}

func TestRegisterFormatting(t *testing.T) {
	s := models.RegState{Memory: 4, N: 3, Overwritten: 1<<0 | 1<<1, StaleRead: true, C: [3]models.RegClient{
		{Phase: models.RegReading, Cached: true, Cache: 1},
		{Phase: models.RegWriting, Arg: 4},
		{Cached: true, Cache: 4},
	}}
	const want = "memory=4 | A reading, cache=1 | B writing 4, cache empty | C idle, cache=4" +
		" | overwritten: {0,1} | STALE READ: a read returned a value overwritten before it started"
	if got := s.String(); got != want {
		t.Errorf("state:\n got %q\nwant %q", got, want)
	}
	for a, want := range map[models.RegAction]string{
		{Client: 2, Start: true}:                        "C starts read",
		{Client: 1, Start: true, Write: true, Value: 3}: "B starts write(3)",
		{Client: 1, Write: true, Value: 3}:              "B write(3) done: memory = 3",
		{Client: 0, Value: 2}:                           "A read returns 2 (cache miss, loaded from memory)",
		{Client: 0, Value: 0, Hit: true}:                "A read returns 0 (cache hit)",
	} {
		if got := a.String(); got != want {
			t.Errorf("action %+v = %q, want %q", a, got, want)
		}
	}
}

func TestRegisterRejectsBadSize(t *testing.T) {
	res := runRegister(t, models.Register{Clients: 4, Ops: 1})
	if res.Status != check.ModelError {
		t.Errorf("status = %v, want ModelError", res.Status)
	}
}
