package check_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/reference"
	"github.com/stevenstank/atlas/models"
)

// row is one row of docs/CONFORMANCE.md. d and n are -1 for "no limit";
// cancel is the number of expansions after which cancellation is requested
// (-1: never).
type row struct {
	id, model, inv    string
	d, n, cancel      int
	status            check.Status
	reason            check.StopReason
	ie, ia, idup      int64
	adm, exp, tr, dup int64
	cut, slr          int64
	phase             check.Phase
	maxDepth          int
}

const (
	E, B, I, V, ME = check.Exhausted, check.Bounded, check.Incomplete, check.Violation, check.ModelError
	no, dl, sl, cn = check.NoReason, check.DepthLimit, check.StateLimit, check.Canceled
	np, ip, xp     = check.NoPhase, check.InitPhase, check.ExpansionPhase
)

var rows = []row{
	{"J1", "jugs", "", -1, -1, -1, E, no, 1, 1, 0, 16, 16, 58, 43, 0, 0, np, 7},
	{"J2", "jugs", "NotFour", -1, -1, -1, V, no, 1, 1, 0, 13, 11, 38, 26, 0, 0, np, 6},
	{"J3", "jugs", "", 0, -1, -1, B, dl, 1, 1, 0, 1, 1, 2, 0, 2, 0, np, 0},
	{"J4", "jugs", "", 5, -1, -1, B, dl, 1, 1, 0, 12, 12, 42, 29, 2, 0, np, 5},
	{"J5", "jugs", "", 6, -1, -1, B, dl, 1, 1, 0, 14, 14, 50, 35, 2, 0, np, 6},
	{"J6", "jugs", "", 7, -1, -1, E, no, 1, 1, 0, 16, 16, 58, 43, 0, 0, np, 7},
	{"J7", "jugs", "", 8, -1, -1, E, no, 1, 1, 0, 16, 16, 58, 43, 0, 0, np, 7},
	{"J8", "jugs", "NotFour", 5, -1, -1, B, dl, 1, 1, 0, 12, 12, 42, 29, 2, 0, np, 5},
	{"J9", "jugs", "NotFour", 6, -1, -1, V, no, 1, 1, 0, 13, 11, 38, 26, 0, 0, np, 6},
	{"J10", "jugs", "", -1, 1, -1, B, sl, 1, 1, 0, 1, 1, 1, 0, 0, 1, xp, 0},
	{"J11", "jugs", "", -1, 15, -1, B, sl, 1, 1, 0, 15, 14, 48, 33, 0, 1, xp, 7},
	{"J12", "jugs", "", -1, 16, -1, E, no, 1, 1, 0, 16, 16, 58, 43, 0, 0, np, 7},
	{"J13", "jugs", "NotFour", -1, 12, -1, B, sl, 1, 1, 0, 12, 11, 38, 26, 0, 1, xp, 5},
	{"J14", "jugs", "NotFour", -1, 13, -1, V, no, 1, 1, 0, 13, 11, 38, 26, 0, 0, np, 6},
	{"J15", "jugs", "", 6, 14, -1, B, dl, 1, 1, 0, 14, 14, 50, 35, 2, 0, np, 6},
	{"J16", "jugs", "", 6, 13, -1, B, sl, 1, 1, 0, 13, 12, 42, 29, 0, 1, xp, 6},
	{"J17", "jugs", "", 7, 16, -1, E, no, 1, 1, 0, 16, 16, 58, 43, 0, 0, np, 7},
	{"J18", "jugs", "NotFour", 5, 12, -1, B, dl, 1, 1, 0, 12, 12, 42, 29, 2, 0, np, 5},
	{"J19", "jugs", "", 5, -1, 11, I, cn, 1, 1, 0, 12, 11, 38, 26, 1, 0, np, 5},
	{"J20", "jugs", "", 5, -1, 12, B, dl, 1, 1, 0, 12, 12, 42, 29, 2, 0, np, 5},
	{"G1", "grid", "", -1, -1, -1, E, no, 1, 1, 0, 9, 9, 12, 4, 0, 0, np, 4},
	{"G2", "grid", "", 3, -1, -1, B, dl, 1, 1, 0, 8, 8, 12, 3, 2, 0, np, 3},
	{"G3", "grid", "", 4, -1, -1, E, no, 1, 1, 0, 9, 9, 12, 4, 0, 0, np, 4},
	{"G4", "grid", "", -1, 9, -1, E, no, 1, 1, 0, 9, 9, 12, 4, 0, 0, np, 4},
	{"G5", "grid", "", -1, 8, -1, B, sl, 1, 1, 0, 8, 7, 11, 3, 0, 1, xp, 3},
	{"G6", "grid", "", 3, 8, -1, B, dl, 1, 1, 0, 8, 8, 12, 3, 2, 0, np, 3},
	{"M1", "multi", "", -1, -1, -1, E, no, 5, 4, 1, 9, 9, 12, 7, 0, 0, np, 2},
	{"M2", "multi", "Sum", -1, -1, -1, V, no, 5, 4, 1, 4, 0, 0, 0, 0, 0, np, 0},
	{"M3", "multi", "", -1, 2, -1, B, sl, 4, 2, 1, 2, 0, 0, 0, 0, 1, ip, 0},
	{"M4", "multi", "Sum", -1, 2, -1, B, sl, 4, 2, 1, 2, 0, 0, 0, 0, 1, ip, 0},
	{"M5", "multi", "", -1, 3, -1, B, sl, 5, 3, 1, 3, 0, 0, 0, 0, 1, ip, 0},
	{"M6", "multi", "", -1, 4, -1, B, sl, 5, 4, 1, 4, 2, 3, 2, 0, 1, xp, 0},
	{"M7", "multi", "", 0, -1, -1, B, dl, 5, 4, 1, 4, 4, 6, 2, 4, 0, np, 0},
	{"M8", "multi", "", -1, 9, -1, E, no, 5, 4, 1, 9, 9, 12, 7, 0, 0, np, 2},
	{"M9", "multi", "Sum", -1, 4, -1, V, no, 5, 4, 1, 4, 0, 0, 0, 0, 0, np, 0},
	{"E1", "empty", "Never", -1, -1, -1, ME, no, 0, 0, 0, 0, 0, 0, 0, 0, 0, np, -1},
	{"E2", "empty", "Never", 0, 1, -1, ME, no, 0, 0, 0, 0, 0, 0, 0, 0, 0, np, -1},
}

func TestConformance(t *testing.T) {
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			switch r.model {
			case "jugs":
				checkRow(t, r, models.Jugs{}, models.NotFour)
			case "grid":
				checkRow(t, r, models.Origin, models.Sum)
			case "multi":
				checkRow(t, r, models.MultiInit, models.Sum)
			case "empty":
				checkRow(t, r, models.Grid2{Inits: []models.Cell{}}, func(models.Cell) bool { return false })
			}
		})
	}
}

// cancelAfter cancels the run's context at the end of the k-th Next call.
type cancelAfter[S, A any] struct {
	core.Model[S, A]
	k, calls int
	cancel   func()
}

func (c *cancelAfter[S, A]) Next(s S, emit func(A, S) bool) {
	c.Model.Next(s, emit)
	if c.calls++; c.calls == c.k {
		c.cancel()
	}
}

func checkRow[S, A any](t *testing.T, r row, m core.Model[S, A], namedInv func(S) bool) {
	t.Helper()
	cfg := check.Config[S]{CheckInterval: 1}
	var inv func(S) bool
	if r.inv != "" {
		inv = namedInv
		cfg.Invariants = []check.Invariant[S]{{Name: r.inv, Holds: inv}}
	}
	if r.d >= 0 {
		cfg.MaxDepth = check.Max(r.d)
	}
	if r.n >= 0 {
		cfg.MaxStates = check.Max(r.n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := m
	if r.cancel >= 0 {
		run = &cancelAfter[S, A]{Model: m, k: r.cancel, cancel: cancel}
	}
	res, err := check.Run(ctx, run, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	st := res.Stats
	got := row{r.id, r.model, r.inv, r.d, r.n, r.cancel, res.Status, res.Reason,
		st.InitEmissions, st.InitAdmitted, st.InitDuplicates, st.Admitted, st.Expanded,
		st.Transitions, st.Duplicates, st.CutoffTransitions, st.StateLimitRefusals, st.RefusalPhase, st.MaxDepth}
	if got != r {
		t.Errorf("engine mismatch\n got  %+v\n want %+v", got, r)
	}

	// I1 and I2 hold on every run (D-012 counting rule).
	rInit, rExp := int64(0), int64(0)
	if st.RefusalPhase == check.InitPhase {
		rInit = st.StateLimitRefusals
	} else {
		rExp = st.StateLimitRefusals
	}
	if st.InitEmissions != st.InitAdmitted+st.InitDuplicates+rInit {
		t.Errorf("I1 fails: %+v", st)
	}
	if st.Transitions != (st.Admitted-st.InitAdmitted)+st.Duplicates+st.CutoffTransitions+rExp {
		t.Errorf("I2 fails: %+v", st)
	}

	if r.cancel >= 0 {
		return // the reference explorer does not model interruption
	}
	ref := reference.Explore(m, inv, r.d, r.n)
	refRow := row{r.id, r.model, r.inv, r.d, r.n, r.cancel, refStatus(ref), refReason(ref),
		int64(ref.InitEmissions), int64(ref.InitAdmitted), int64(ref.InitDuplicates), int64(ref.Admitted),
		int64(ref.Expanded), int64(ref.Transitions), int64(ref.Duplicates), int64(ref.Cutoffs),
		int64(ref.Refusals), refPhase(ref), ref.MaxDepth}
	if refRow != r {
		t.Errorf("reference mismatch\n got  %+v\n want %+v", refRow, r)
	}
	// I3 only for Exhausted; I4 only after normal completion.
	if res.Status == check.Exhausted {
		full := reference.Explore(m, nil, -1, -1)
		if st.Admitted != int64(full.Admitted) || st.Transitions != int64(full.Edges) {
			t.Errorf("I3 fails: admitted %d transitions %d, graph %d states %d edges",
				st.Admitted, st.Transitions, full.Admitted, full.Edges)
		}
	}
	if res.Status == check.Exhausted || (res.Status == check.Bounded && res.Reason == check.DepthLimit) {
		if st.Transitions != int64(ref.SuccessorSum) {
			t.Errorf("I4 fails: transitions %d, successor sum %d", st.Transitions, ref.SuccessorSum)
		}
	}
	if res.Status == check.Violation {
		if n := len(res.Violation.Trace.Steps) - 1; n != ref.ViolationDepth {
			t.Errorf("trace has %d steps, minimal violation depth is %d", n, ref.ViolationDepth)
		}
		checkTrace(t, m, res.Violation.Trace, inv)
	}
}

// checkTrace replays a trace independently: it starts at an initial state,
// each step is an emission of the previous state's Next, and it ends in a
// state that violates inv.
func checkTrace[S, A any](t *testing.T, m core.Model[S, A], tr check.Trace[S, A], inv func(S) bool) {
	t.Helper()
	key := func(s S) string { return string(m.AppendKey(nil, s)) }
	steps := tr.Steps
	found := false
	m.Init(func(s S) bool { found = found || key(s) == key(steps[0].State); return true })
	for i := 1; i < len(steps) && found; i++ {
		found = false
		m.Next(steps[i-1].State, func(a A, s S) bool {
			found = found || (any(a) == any(steps[i].Action) && key(s) == key(steps[i].State))
			return true
		})
	}
	if !found || inv(steps[len(steps)-1].State) {
		t.Errorf("trace does not replay to a violating state: %+v", steps)
	}
}

func refStatus(c reference.Counts) check.Status {
	return map[string]check.Status{"Exhausted": E, "Bounded": B, "Violation": V, "ModelError": ME}[c.Status]
}

func refReason(c reference.Counts) check.StopReason {
	switch {
	case c.Status != "Bounded":
		return no
	case c.StateLimited:
		return sl
	}
	return dl
}

func refPhase(c reference.Counts) check.Phase {
	switch {
	case c.Refusals == 0:
		return np
	case c.RefusedInInit:
		return ip
	}
	return xp
}
