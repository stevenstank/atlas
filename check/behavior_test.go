package check_test

import (
	"context"
	"errors"
	"fmt"
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

// A Next callback retained from one expansion and called during a later one
// is a detected contract violation (D-001) and ends the run (D-012). The
// result stays ModelError: an emission on the current call's own callback
// after the detection is refused and not counted, whatever it would have
// caused.
func TestRegression_RetainedNextCallbackIsTerminal(t *testing.T) {
	target := models.Cell{X: 2, Y: 2}
	notTarget := []check.Invariant[models.Cell]{{Name: "NotTarget", Holds: func(c models.Cell) bool { return c != target }}}
	for _, c := range []struct {
		name string
		cfg  cellCfg
	}{
		{"then violation", cellCfg{Invariants: notTarget}},
		{"then state-limit refusal", cellCfg{MaxStates: check.Max(2)}},
		{"then new state", cellCfg{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var saved func(string, models.Cell) bool
			stale, later := true, true
			m := hooked[models.Cell, string]{Model: models.Origin, next: func(s models.Cell, emit func(string, models.Cell) bool) {
				switch s {
				case models.Cell{}:
					saved = emit
					emit("IncX", models.Cell{X: 1})
				case models.Cell{X: 1}:
					stale = saved("stale", models.Cell{Y: 1})
					later = emit("later", target)
				}
			}}
			res := run(t, context.Background(), m, c.cfg)
			st := res.Stats
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			if stale || later {
				t.Errorf("stale emit returned %v, later emit returned %v; want false, false", stale, later)
			}
			if st.InitEmissions != 1 || st.InitAdmitted != 1 || st.InitDuplicates != 0 || st.Admitted != 2 ||
				st.Expanded != 2 || st.Transitions != 1 || st.Duplicates != 0 || st.CutoffTransitions != 0 ||
				st.StateLimitRefusals != 0 {
				t.Errorf("counts %+v", st)
			}
			// I1 and I2 (D-012); no refusal in either phase.
			if st.InitEmissions != st.InitAdmitted+st.InitDuplicates ||
				st.Transitions != (st.Admitted-st.InitAdmitted)+st.Duplicates+st.CutoffTransitions {
				t.Errorf("I1 or I2 does not hold: %+v", st)
			}
		})
	}
}

// checkStats compares every counted statistic (all but WallTime) and checks
// I1 and I2 (D-012).
func checkStats(t *testing.T, got, want check.Stats) {
	t.Helper()
	got.WallTime = 0
	if got != want {
		t.Errorf("stats\n got %+v\nwant %+v", got, want)
	}
	var rInit, rExp int64
	switch got.RefusalPhase {
	case check.InitPhase:
		rInit = got.StateLimitRefusals
	case check.ExpansionPhase:
		rExp = got.StateLimitRefusals
	}
	if got.InitEmissions != got.InitAdmitted+got.InitDuplicates+rInit ||
		got.Transitions != (got.Admitted-got.InitAdmitted)+got.Duplicates+got.CutoffTransitions+rExp {
		t.Errorf("I1 or I2 does not hold: %+v", got)
	}
}

// A Next callback retained from the first expansion and called during the
// third, after that call's own first emission, is detected (D-001) although
// another expansion ran in between. The current callback's next emission
// would be a violation; it is refused and not counted.
func TestRegression_RetainedNextCallbackFromEarlierExpansion(t *testing.T) {
	var saved func(string, models.Cell) bool
	var rets []bool
	m := hooked[models.Cell, string]{Model: models.Origin, next: func(s models.Cell, emit func(string, models.Cell) bool) {
		switch s {
		case models.Cell{}:
			saved = emit
		case models.Cell{Y: 1}: // the third expansion
			rets = append(rets, emit("IncX", models.Cell{X: 1, Y: 1}))
			rets = append(rets, saved("stale", models.Cell{X: 2, Y: 2}))
			rets = append(rets, emit("IncY", models.Cell{Y: 2}))
			return
		}
		models.Origin.Next(s, emit)
	}}
	notY2 := []check.Invariant[models.Cell]{{Name: "NotY2", Holds: func(c models.Cell) bool { return c != models.Cell{Y: 2} }}}
	res := run(t, context.Background(), m, cellCfg{Invariants: notY2})
	if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
		t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
	}
	if want := []bool{true, false, false}; !reflect.DeepEqual(rets, want) {
		t.Errorf("emit returned %v, want %v", rets, want)
	}
	checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 5, Expanded: 3,
		Transitions: 5, Duplicates: 1, MaxDepth: 2, FrontierPeak: 3})
}

// A Next callback called between expansions, from the AppendKey that
// DetectMutation runs on the next dequeued state, is detected (D-001). No
// later Next call has started yet, so the callback still belongs to the
// latest call; it is invalid because that call has returned. The detection
// ends the run there: the dequeued state is not expanded, and a key mismatch
// found by the same check does not replace the error.
func TestRegression_RetainedNextCallbackBetweenExpansions(t *testing.T) {
	for _, mutated := range []bool{false, true} {
		t.Run(fmt.Sprintf("mutated %v", mutated), func(t *testing.T) {
			var saved func(string, models.Cell) bool
			armed, stale, nexts := false, true, 0
			m := hooked[models.Cell, string]{Model: models.Origin,
				next: func(s models.Cell, emit func(string, models.Cell) bool) {
					nexts++
					models.Origin.Next(s, emit)
					if s == (models.Cell{}) {
						saved, armed = emit, true
					}
				},
				key: func(buf []byte, c models.Cell) []byte {
					if armed { // the mutation check of (1,0), before its Next call
						armed = false
						stale = saved("stale", models.Cell{X: 2, Y: 2})
						if mutated {
							return append(buf, 9, 9)
						}
					}
					return models.Origin.AppendKey(buf, c)
				}}
			res := run(t, context.Background(), m, cellCfg{DetectMutation: true})
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			if stale || nexts != 1 {
				t.Errorf("stale emit returned %v, Next called %d times; want false, 1", stale, nexts)
			}
			checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 3, Expanded: 1,
				Transitions: 2, MaxDepth: 1, FrontierPeak: 2})
		})
	}
}

// A Next callback retained from the first expansion and called from
// AppendKey while the engine encodes an emission of the second ends the run
// before that emission is examined (D-012 counting rule): it is not counted
// in any branch, emit returns false, and the run is a ModelError whatever
// the emission would have become.
func TestRegression_RetainedNextCallbackFromAppendKey(t *testing.T) {
	notX2 := []check.Invariant[models.Cell]{{Name: "NotX2", Holds: func(c models.Cell) bool { return c != models.Cell{X: 2} }}}
	for _, c := range []struct {
		name string
		cfg  cellCfg
	}{
		{"would be admitted", cellCfg{}},
		{"would be a violation", cellCfg{Invariants: notX2}},
		{"would be refused by the state limit", cellCfg{MaxStates: check.Max(3)}},
		{"would be cut off by the depth limit", cellCfg{MaxDepth: check.Max(1)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var saved func(string, models.Cell) bool
			var rets []bool
			second, stale := false, true
			m := hooked[models.Cell, string]{Model: models.Origin,
				next: func(s models.Cell, emit func(string, models.Cell) bool) {
					switch s {
					case models.Cell{}:
						saved = emit
						models.Origin.Next(s, emit)
					case models.Cell{X: 1}: // the second expansion
						second = true
						rets = append(rets, emit("IncX", models.Cell{X: 2}))
						if rets[0] {
							rets = append(rets, emit("IncY", models.Cell{X: 1, Y: 1}))
						}
						second = false
					}
				},
				key: func(buf []byte, s models.Cell) []byte {
					if second && s == (models.Cell{X: 2}) {
						stale = saved("stale", models.Cell{X: 2, Y: 2})
					}
					return models.Origin.AppendKey(buf, s)
				}}
			res := run(t, context.Background(), m, c.cfg)
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			if want := []bool{false}; stale || !reflect.DeepEqual(rets, want) {
				t.Errorf("stale emit returned %v, emit returned %v; want false, %v", stale, rets, want)
			}
			checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 3, Expanded: 2,
				Transitions: 2, MaxDepth: 1, FrontierPeak: 2})
		})
	}
}

// A Next callback retained from the first expansion and called from an
// invariant during the second is detected (D-001), whatever the invariant
// then returns. The state being checked stays counted as admitted (D-012),
// no later invariant runs on it, emit returns false, and the run is a
// ModelError, not a Violation.
func TestRegression_RetainedNextCallbackFromInvariant(t *testing.T) {
	for _, holds := range []bool{true, false} {
		t.Run(fmt.Sprintf("invariant returns %v", holds), func(t *testing.T) {
			var saved func(string, models.Cell) bool
			var rets []bool
			stale, calls, later := true, 0, 0
			m := hooked[models.Cell, string]{Model: models.Origin, next: func(s models.Cell, emit func(string, models.Cell) bool) {
				switch s {
				case models.Cell{}:
					saved = emit
					models.Origin.Next(s, emit)
				case models.Cell{X: 1}: // the second expansion
					if calls++; calls > 1 { // trace replay, if the run (wrongly) has a trace
						models.Origin.Next(s, emit)
						return
					}
					rets = append(rets, emit("IncX", models.Cell{X: 2}))
					if rets[0] {
						rets = append(rets, emit("IncY", models.Cell{X: 1, Y: 1}))
					}
				}
			}}
			inv := []check.Invariant[models.Cell]{
				{Name: "Calls", Holds: func(c models.Cell) bool {
					if c != (models.Cell{X: 2}) {
						return true
					}
					stale = saved("stale", models.Cell{X: 2, Y: 2})
					return holds
				}},
				{Name: "Later", Holds: func(c models.Cell) bool {
					if c == (models.Cell{X: 2}) {
						later++
					}
					return true
				}},
			}
			res := run(t, context.Background(), m, cellCfg{Invariants: inv})
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			if want := []bool{false}; stale || !reflect.DeepEqual(rets, want) {
				t.Errorf("stale emit returned %v, emit returned %v; want false, %v", stale, rets, want)
			}
			if later != 0 {
				t.Errorf("a later invariant ran %d times on the state after the run ended", later)
			}
			checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 4, Expanded: 2,
				Transitions: 3, MaxDepth: 2, FrontierPeak: 2})
		})
	}
}

// An emit call from AppendKey while the engine encodes another emission of
// the same Next call is a nested emission, a contract violation (D-001). It
// is refused before it can touch the key buffer: unchecked, it overwrote the
// outer key, so (1,0) was taken for a duplicate of the nested (0,2) and the
// run was Exhausted with 7 of Grid2's 9 states. Neither emission is counted,
// whatever the nested one would have become.
func TestRegression_NestedEmitFromAppendKey(t *testing.T) {
	notY2 := []check.Invariant[models.Cell]{{Name: "NotY2", Holds: func(c models.Cell) bool { return c != models.Cell{Y: 2} }}}
	for _, appendFirst := range []bool{true, false} {
		for _, c := range []struct {
			name string
			cfg  cellCfg
		}{
			{"would be admitted", cellCfg{}},
			{"would be a violation", cellCfg{Invariants: notY2}},
			{"would be refused by the state limit", cellCfg{MaxStates: check.Max(1)}},
		} {
			t.Run(fmt.Sprintf("append first %v, %s", appendFirst, c.name), func(t *testing.T) {
				var cur func(string, models.Cell) bool
				var rets, nested []bool
				m := hooked[models.Cell, string]{Model: models.Origin,
					next: func(s models.Cell, emit func(string, models.Cell) bool) {
						cur = emit
						models.Origin.Next(s, func(a string, c models.Cell) bool {
							ok := emit(a, c)
							rets = append(rets, ok)
							return ok
						})
						cur = nil
					},
					key: func(buf []byte, s models.Cell) []byte {
						if cur == nil || s != (models.Cell{X: 1}) || len(nested) > 0 {
							return models.Origin.AppendKey(buf, s)
						}
						if appendFirst {
							buf = models.Origin.AppendKey(buf, s)
							nested = append(nested, cur("nested", models.Cell{Y: 2}))
							return buf
						}
						nested = append(nested, cur("nested", models.Cell{Y: 2}))
						return models.Origin.AppendKey(buf, s)
					}}
				res := run(t, context.Background(), m, c.cfg)
				if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
					t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
				}
				if !reflect.DeepEqual(nested, []bool{false}) || !reflect.DeepEqual(rets, []bool{false}) {
					t.Errorf("nested emit returned %v, outer emit returned %v; want [false], [false]", nested, rets)
				}
				checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 1, Expanded: 1,
					MaxDepth: 0, FrontierPeak: 1})
			})
		}
	}
}

// An emit call from an invariant while the engine checks an admitted state
// is a nested emission (D-001), whatever the invariant then returns. The
// checked state stays counted as admitted (D-012); the nested emission is
// not counted, no later invariant runs, and the run is not a Violation.
func TestRegression_NestedEmitFromInvariant(t *testing.T) {
	for _, holds := range []bool{true, false} {
		t.Run(fmt.Sprintf("invariant returns %v", holds), func(t *testing.T) {
			var cur func(string, models.Cell) bool
			var rets, nested []bool
			later := 0
			m := hooked[models.Cell, string]{Model: models.Origin, next: func(s models.Cell, emit func(string, models.Cell) bool) {
				cur = emit
				models.Origin.Next(s, func(a string, c models.Cell) bool {
					ok := emit(a, c)
					rets = append(rets, ok)
					return ok
				})
				cur = nil
			}}
			inv := []check.Invariant[models.Cell]{
				{Name: "Nests", Holds: func(c models.Cell) bool {
					if cur == nil || c != (models.Cell{X: 1}) || len(nested) > 0 {
						return true
					}
					nested = append(nested, cur("nested", models.Cell{Y: 2}))
					return holds
				}},
				{Name: "Later", Holds: func(c models.Cell) bool {
					if c == (models.Cell{X: 1}) {
						later++
					}
					return true
				}},
			}
			res := run(t, context.Background(), m, cellCfg{Invariants: inv})
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			if !reflect.DeepEqual(nested, []bool{false}) || !reflect.DeepEqual(rets[:1], []bool{false}) {
				t.Errorf("nested emit returned %v, outer emit returned %v; want [false], [false ...]", nested, rets)
			}
			if later != 0 {
				t.Errorf("a later invariant ran %d times on the state after the run ended", later)
			}
			checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 2, Expanded: 1,
				Transitions: 1, MaxDepth: 1, FrontierPeak: 1})
		})
	}
}

// A nested emission during Init (from AppendKey while the engine encodes the
// second initial state) is refused the same way. The first initial state
// stays counted; the interrupted second one is not examined (I1).
func TestRegression_NestedEmitDuringInit(t *testing.T) {
	var cur func(models.Cell) bool
	var rets, nested []bool
	two := models.Grid2{Inits: []models.Cell{{}, {X: 1}}}
	m := hooked[models.Cell, string]{Model: two,
		init: func(emit func(models.Cell) bool) {
			cur = emit
			two.Init(func(c models.Cell) bool {
				ok := emit(c)
				rets = append(rets, ok)
				return ok
			})
			cur = nil
		},
		key: func(buf []byte, s models.Cell) []byte {
			if cur != nil && s == (models.Cell{X: 1}) && len(nested) == 0 {
				nested = append(nested, cur(models.Cell{Y: 2}))
			}
			return models.Origin.AppendKey(buf, s)
		}}
	res := run(t, context.Background(), m, cellCfg{})
	if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
		t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
	}
	if !reflect.DeepEqual(nested, []bool{false}) || !reflect.DeepEqual(rets, []bool{true, false}) {
		t.Errorf("nested emit returned %v, Init's emit returned %v; want [false], [true false]", nested, rets)
	}
	checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 1, MaxDepth: 0, FrontierPeak: 1})
}

// After a nested emission is detected, further emissions, nested or not,
// are refused and counted nowhere, and the error stays ErrCallbackContract.
func TestRegression_EmitAfterNestedEmit(t *testing.T) {
	var cur func(string, models.Cell) bool
	var rets, nested []bool
	m := hooked[models.Cell, string]{Model: models.Origin,
		next: func(s models.Cell, emit func(string, models.Cell) bool) {
			cur = emit
			rets = append(rets, emit("IncX", models.Cell{X: s.X + 1, Y: s.Y})) // ignores false
			rets = append(rets, emit("IncY", models.Cell{X: s.X, Y: s.Y + 1}))
			cur = nil
		},
		key: func(buf []byte, s models.Cell) []byte {
			if cur != nil && s == (models.Cell{X: 1}) && len(nested) == 0 {
				nested = append(nested, cur("nested", models.Cell{Y: 2}), cur("again", models.Cell{X: 2}))
			}
			return models.Origin.AppendKey(buf, s)
		}}
	notY1 := []check.Invariant[models.Cell]{{Name: "NotY1", Holds: func(c models.Cell) bool { return c != models.Cell{Y: 1} }}}
	res := run(t, context.Background(), m, cellCfg{Invariants: notY1})
	if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) {
		t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
	}
	if !reflect.DeepEqual(nested, []bool{false, false}) || !reflect.DeepEqual(rets, []bool{false, false}) {
		t.Errorf("nested emits returned %v, Next's emits returned %v; want all false", nested, rets)
	}
	checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 1, Expanded: 1,
		MaxDepth: 0, FrontierPeak: 1})
}

// Emitting through wrappers, and calling AppendKey from the model's own
// Next, are ordinary sequential emissions: the results are identical to the
// plain model's, with and without a violation and its replayed trace.
func TestSequentialEmitsStayValid(t *testing.T) {
	m := hooked[models.Cell, string]{Model: models.Origin, next: func(s models.Cell, emit func(string, models.Cell) bool) {
		_ = models.Origin.AppendKey(nil, s)
		models.Origin.Next(s, func(a string, c models.Cell) bool { return emit(a, c) })
	}}
	sum := []check.Invariant[models.Cell]{{Name: "Sum", Holds: models.Sum}}
	for _, cfg := range []cellCfg{{}, {Invariants: sum}, {MaxDepth: check.Max(2)}, {DetectMutation: true}} {
		got, want := run(t, context.Background(), m, cfg), run(t, context.Background(), models.Origin, cfg)
		got.Stats.WallTime, want.Stats.WallTime = 0, 0
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%+v:\n got %+v\nwant %+v", cfg, got, want)
		}
	}
}

// A model panic after a detected contract violation does not replace it:
// the result stays ErrCallbackContract (D-001 amendment), and the counters
// are those at the panic.
func TestRegression_PanicAfterCallbackMisuse(t *testing.T) {
	type hooks struct {
		next func(saved func(string, models.Cell) bool, s models.Cell, emit func(string, models.Cell) bool)
		key  func(saved func(string, models.Cell) bool, s models.Cell)
		inv  func(saved func(string, models.Cell) bool, s models.Cell)
	}
	misuse := func(saved func(string, models.Cell) bool) {
		saved("stale", models.Cell{X: 2, Y: 2})
		panic("after misuse")
	}
	second := false // in the second expansion, of (1,0)
	for _, c := range []struct {
		name string
		h    hooks
		cfg  cellCfg
		want check.Stats
	}{
		{"invariant", hooks{inv: func(saved func(string, models.Cell) bool, s models.Cell) {
			if s == (models.Cell{X: 2}) {
				misuse(saved)
			}
		}}, cellCfg{}, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 4, Expanded: 2, Transitions: 3, MaxDepth: 2, FrontierPeak: 2}},
		{"AppendKey during an emission", hooks{key: func(saved func(string, models.Cell) bool, s models.Cell) {
			if second && s == (models.Cell{X: 2}) {
				misuse(saved)
			}
		}}, cellCfg{}, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 3, Expanded: 2, Transitions: 2, MaxDepth: 1, FrontierPeak: 2}},
		{"Next", hooks{next: func(saved func(string, models.Cell) bool, s models.Cell, _ func(string, models.Cell) bool) {
			if s == (models.Cell{X: 1}) {
				misuse(saved)
			}
		}}, cellCfg{}, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 3, Expanded: 2, Transitions: 2, MaxDepth: 1, FrontierPeak: 2}},
		{"AppendKey during DetectMutation", hooks{key: func(saved func(string, models.Cell) bool, s models.Cell) {
			if saved != nil && !second && s == (models.Cell{X: 1}) {
				misuse(saved)
			}
		}}, cellCfg{DetectMutation: true}, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 3, Expanded: 1, Transitions: 2, MaxDepth: 1, FrontierPeak: 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var saved func(string, models.Cell) bool
			second = false
			m := hooked[models.Cell, string]{Model: models.Origin,
				next: func(s models.Cell, emit func(string, models.Cell) bool) {
					if c.h.next != nil {
						c.h.next(saved, s, emit)
					}
					second = s == (models.Cell{X: 1})
					models.Origin.Next(s, emit)
					second = false
					if s == (models.Cell{}) {
						saved = emit
					}
				},
				key: func(buf []byte, s models.Cell) []byte {
					if c.h.key != nil {
						c.h.key(saved, s)
					}
					return models.Origin.AppendKey(buf, s)
				}}
			cfg := c.cfg
			if c.h.inv != nil {
				cfg.Invariants = []check.Invariant[models.Cell]{{Name: "Misuses", Holds: func(s models.Cell) bool {
					c.h.inv(saved, s)
					return true
				}}}
			}
			res := run(t, context.Background(), m, cfg)
			var pe *check.PanicError
			if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) || errors.As(res.Err, &pe) {
				t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
			}
			checkStats(t, res.Stats, c.want)
		})
	}
}

// A nested emission followed by a panic in the same AppendKey call is
// reported as the nested emission, not the panic.
func TestRegression_PanicAfterNestedEmit(t *testing.T) {
	var cur func(string, models.Cell) bool
	nested := true
	m := hooked[models.Cell, string]{Model: models.Origin,
		next: func(s models.Cell, emit func(string, models.Cell) bool) {
			cur = emit
			models.Origin.Next(s, emit)
			cur = nil
		},
		key: func(buf []byte, s models.Cell) []byte {
			if cur != nil && s == (models.Cell{X: 1}) {
				nested = cur("nested", models.Cell{Y: 2})
				panic("after nested emit")
			}
			return models.Origin.AppendKey(buf, s)
		}}
	res := run(t, context.Background(), m, cellCfg{})
	var pe *check.PanicError
	if res.Status != check.ModelError || !errors.Is(res.Err, check.ErrCallbackContract) || errors.As(res.Err, &pe) {
		t.Errorf("got %v %v, want ModelError %v", res.Status, res.Err, check.ErrCallbackContract)
	}
	if nested {
		t.Error("nested emit returned true")
	}
	checkStats(t, res.Stats, check.Stats{InitEmissions: 1, InitAdmitted: 1, Admitted: 1, Expanded: 1,
		MaxDepth: 0, FrontierPeak: 1})
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
