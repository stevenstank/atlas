package check_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/models"
)

type (
	jugCfg  = check.Config[models.Jug]
	cellCfg = check.Config[models.Cell]
)

var notFour = []check.Invariant[models.Jug]{{Name: "NotFour", Holds: models.NotFour}}

func run[S, A any](t *testing.T, ctx context.Context, m core.Model[S, A], cfg check.Config[S]) check.Result[S, A] {
	t.Helper()
	res, err := check.Run(ctx, m, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// hooked wraps a model with optional hooks around Init, Next, and AppendKey.
type hooked[S, A any] struct {
	core.Model[S, A]
	init func(emit func(S) bool)
	next func(s S, emit func(A, S) bool)
	key  func(buf []byte, s S) []byte
}

func (h hooked[S, A]) Init(emit func(S) bool) {
	if h.init != nil {
		h.init(emit)
		return
	}
	h.Model.Init(emit)
}

func (h hooked[S, A]) Next(s S, emit func(A, S) bool) {
	if h.next != nil {
		h.next(s, emit)
		return
	}
	h.Model.Next(s, emit)
}

func (h hooked[S, A]) AppendKey(buf []byte, s S) []byte {
	if h.key != nil {
		return h.key(buf, s)
	}
	return h.Model.AppendKey(buf, s)
}

func TestInvalidConfigRejectedBeforeInit(t *testing.T) {
	called := false
	m := hooked[models.Jug, string]{Model: models.Jugs{}, init: func(func(models.Jug) bool) { called = true }}
	for _, cfg := range []jugCfg{
		{MaxDepth: check.Max(-1)},
		{MaxStates: check.Max(0)},
		{CheckInterval: -1},
		{Invariants: []check.Invariant[models.Jug]{{Name: "nil"}}},
	} {
		if _, err := check.Run(context.Background(), m, cfg); !errors.Is(err, check.ErrInvalidConfig) {
			t.Errorf("%+v: err = %v, want ErrInvalidConfig", cfg, err)
		}
	}
	if called {
		t.Error("Init was called for an invalid configuration")
	}
	if n, ok := (check.Limit{}).Get(); ok || n != 0 {
		t.Error("zero Limit must be unset")
	}
	if n, ok := check.Max(0).Get(); !ok || n != 0 {
		t.Error("Max(0) must be a set limit of 0")
	}
}

func TestInterruptBeforeInit(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancel2 := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel2()
	for ctx, want := range map[context.Context]check.StopReason{canceled: check.Canceled, expired: check.DeadlineExceeded} {
		called := false
		m := hooked[models.Jug, string]{Model: models.Jugs{}, init: func(func(models.Jug) bool) { called = true }}
		res := run(t, ctx, m, jugCfg{})
		if res.Status != check.Incomplete || res.Reason != want || called || res.Stats.MaxDepth != -1 {
			t.Errorf("got %v/%v called=%v stats=%+v", res.Status, res.Reason, called, res.Stats)
		}
	}
}

func TestCancelDuringInitObservedBeforeFirstDequeue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := hooked[models.Cell, string]{Model: models.MultiInit, init: func(emit func(models.Cell) bool) {
		cancel()
		models.MultiInit.Init(emit)
	}}
	res := run(t, ctx, m, cellCfg{CheckInterval: 1})
	st := res.Stats
	if res.Status != check.Incomplete || res.Reason != check.Canceled ||
		st.InitEmissions != 5 || st.InitAdmitted != 4 || st.InitDuplicates != 1 || st.Expanded != 0 {
		t.Errorf("got %v/%v %+v", res.Status, res.Reason, st)
	}
}

// emit returns true for duplicates and cutoffs, and false only at the
// emission that ends the run. A violation adds one false per Next call
// replayed for the trace (6 for J2), where replay stops at the wanted step.
func TestEmitReturnValues(t *testing.T) {
	for _, c := range []struct {
		cfg   jugCfg
		false int
	}{{jugCfg{MaxDepth: check.Max(5)}, 0}, {jugCfg{Invariants: notFour}, 1 + 6}, {jugCfg{MaxStates: check.Max(15)}, 1}} {
		falses, afterFalse := 0, 0
		m := hooked[models.Jug, string]{Model: models.Jugs{}, next: func(s models.Jug, emit func(string, models.Jug) bool) {
			stopped := false
			models.Jugs{}.Next(s, func(a string, t models.Jug) bool {
				if stopped {
					afterFalse++
				}
				ok := emit(a, t)
				if !ok {
					falses++
					stopped = true
				}
				return ok
			})
		}}
		run(t, context.Background(), m, c.cfg)
		if falses != c.false || afterFalse != 0 {
			t.Errorf("%+v: %d false returns, %d emissions after false", c.cfg, falses, afterFalse)
		}
	}
}

func TestCallbackMisuseIsModelError(t *testing.T) {
	// Ignores false: keeps emitting after a state-limit refusal.
	ignoring := hooked[models.Cell, string]{Model: models.Origin, next: func(c models.Cell, emit func(string, models.Cell) bool) {
		emit("A", models.Cell{X: c.X + 1})
		emit("B", models.Cell{Y: c.Y + 1})
	}}
	// Retains Init's callback and calls it from Next.
	var saved func(models.Cell) bool
	retaining := hooked[models.Cell, string]{Model: models.Origin,
		init: func(emit func(models.Cell) bool) { saved = emit; emit(models.Cell{}) },
		next: func(models.Cell, func(string, models.Cell) bool) { saved(models.Cell{X: 1}) }}
	for name, m := range map[string]core.Model[models.Cell, string]{"ignores false": ignoring, "retained": retaining} {
		res := run(t, context.Background(), m, cellCfg{MaxStates: check.Max(1)})
		if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
			t.Errorf("%s: got %v %v", name, res.Status, res.Err)
		}
	}
}

func TestModelPanicsAreModelErrors(t *testing.T) {
	keys := 0
	panicKey := hooked[models.Jug, string]{Model: models.Jugs{}, key: func(buf []byte, j models.Jug) []byte {
		if keys++; keys == 10 {
			panic("key")
		}
		return models.Jugs{}.AppendKey(buf, j)
	}}
	panicInit := hooked[models.Jug, string]{Model: models.Jugs{}, init: func(func(models.Jug) bool) { panic("init") }}
	panicNext := hooked[models.Jug, string]{Model: models.Jugs{}, next: func(models.Jug, func(string, models.Jug) bool) { panic("next") }}
	for name, m := range map[string]core.Model[models.Jug, string]{"key": panicKey, "init": panicInit, "next": panicNext} {
		res := run(t, context.Background(), m, jugCfg{})
		var pe *check.PanicError
		if res.Status != check.ModelError || !errors.As(res.Err, &pe) {
			t.Errorf("%s: got %v %v", name, res.Status, res.Err)
		}
		st := res.Stats // the failing emission is uncounted, so I2 still holds
		if st.Transitions != (st.Admitted-st.InitAdmitted)+st.Duplicates+st.CutoffTransitions {
			t.Errorf("%s: I2 fails on ModelError: %+v", name, st)
		}
	}
	bad := jugCfg{Invariants: []check.Invariant[models.Jug]{{Name: "boom", Holds: func(models.Jug) bool { panic("inv") }}}}
	if res := run(t, context.Background(), models.Jugs{}, bad); res.Status != check.ModelError || res.Stats.Admitted != 1 {
		t.Errorf("invariant panic: got %v, admitted %d", res.Status, res.Stats.Admitted)
	}
}

// counter emits a different successor on every Next call, so replay fails.
type counter struct{ calls int }

func (c *counter) Init(emit func(int) bool)                { emit(0) }
func (c *counter) Next(_ int, emit func(string, int) bool) { c.calls++; emit("step", c.calls) }
func (c *counter) AppendKey(buf []byte, n int) []byte      { return append(buf, byte(n)) }

func TestNondeterministicReplayIsModelError(t *testing.T) {
	cfg := check.Config[int]{Invariants: []check.Invariant[int]{{Name: "lt3", Holds: func(n int) bool { return n < 3 }}}}
	res := run(t, context.Background(), &counter{}, cfg)
	if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrNondeterministic) {
		t.Errorf("got %v %v", res.Status, res.Err)
	}
}

func TestDeterminism(t *testing.T) {
	for _, cfg := range []jugCfg{{}, {Invariants: notFour}, {MaxDepth: check.Max(5), MaxStates: check.Max(12)}} {
		a, b := run(t, context.Background(), models.Jugs{}, cfg), run(t, context.Background(), models.Jugs{}, cfg)
		a.Stats.WallTime, b.Stats.WallTime = 0, 0
		if !reflect.DeepEqual(a, b) {
			t.Errorf("runs differ:\n%+v\n%+v", a, b)
		}
	}
}

func TestDeadEndIsNotAViolation(t *testing.T) {
	res := run(t, context.Background(), models.Origin, cellCfg{})
	if res.Status != check.Exhausted || res.Stats.TerminalStates != 1 {
		t.Errorf("got %v, terminal states %d", res.Status, res.Stats.TerminalStates)
	}
}

func TestJugTraceMatchesConformance(t *testing.T) {
	res := run(t, context.Background(), models.Jugs{}, jugCfg{Invariants: notFour})
	want := []check.Step[models.Jug, string]{{"", models.Jug{}}, {"FillBig", models.Jug{B: 5}},
		{"BigToSmall", models.Jug{B: 2, S: 3}}, {"EmptySmall", models.Jug{B: 2}}, {"BigToSmall", models.Jug{S: 2}},
		{"FillBig", models.Jug{B: 5, S: 2}}, {"BigToSmall", models.Jug{B: 4, S: 3}}}
	if res.Violation == nil || !reflect.DeepEqual(res.Violation.Trace.Steps, want) {
		t.Errorf("trace = %+v", res.Violation)
	}
	// SEMANTICS.md §6: Grid2 with Sum stops after 9 states, 11 transitions, 3 duplicates.
	g := run(t, context.Background(), models.Origin, cellCfg{Invariants: []check.Invariant[models.Cell]{{Name: "Sum", Holds: models.Sum}}})
	if g.Stats.Admitted != 9 || g.Stats.Transitions != 11 || g.Stats.Duplicates != 3 || len(g.Violation.Trace.Steps) != 5 {
		t.Errorf("Grid2 Sum: %+v", g.Stats)
	}
}

func TestClaimMatchesStatus(t *testing.T) {
	bg := context.Background()
	for want, res := range map[string]check.Result[models.Jug, string]{
		"no reachable state violates": run(t, bg, models.Jugs{}, jugCfg{}),
		"depth <= 5":                  run(t, bg, models.Jugs{}, jugCfg{MaxDepth: check.Max(5)}),
		"15 admitted states, which include every state at depth < 7": run(t, bg, models.Jugs{}, jugCfg{MaxStates: check.Max(15)}),
		`"NotFour" is violated; counterexample has 6 steps`:          run(t, bg, models.Jugs{}, jugCfg{Invariants: notFour}),
	} {
		if !strings.Contains(res.Claim(), want) {
			t.Errorf("Claim() = %q, want it to contain %q", res.Claim(), want)
		}
	}
	m3 := run(t, bg, models.MultiInit, cellCfg{MaxStates: check.Max(2)})
	e1 := run(t, bg, models.Grid2{Inits: []models.Cell{}}, cellCfg{})
	if !strings.Contains(m3.Claim(), "initial set itself was not fully examined") ||
		!strings.Contains(e1.Claim(), "no initial states") || !errors.Is(e1.Err, check.ErrNoInitialStates) {
		t.Errorf("claims: %q / %q", m3.Claim(), e1.Claim())
	}
}
