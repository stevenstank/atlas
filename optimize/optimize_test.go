package optimize_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/stevenstank/atlas/optimize"
)

func solve[V any](t *testing.T, ctx context.Context, p optimize.Problem[V], cfg optimize.Config) optimize.Result[V] {
	t.Helper()
	res, err := optimize.Solve(ctx, p, cfg)
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	return res
}

// knapsack is the worked example of OPTIMIZATION.md §8: items A(2,3), B(3,4),
// C(4,5), capacity 5, maximize value, "exclude before include".
func knapsack() optimize.Problem[bool] {
	weight, value := []int64{2, 3, 4}, []int64{3, 4, 5}
	sum := func(a []bool, w []int64) (s int64) {
		for i, in := range a {
			if in {
				s += w[i]
			}
		}
		return s
	}
	inOut := []bool{false, true}
	return optimize.Problem[bool]{
		Vars:      []optimize.Var[bool]{{"A", inOut}, {"B", inOut}, {"C", inOut}},
		Sense:     optimize.Maximize,
		Feasible:  func(a []bool) bool { return sum(a, weight) <= 5 },
		Objective: func(a []bool) int64 { return sum(a, value) },
	}
}

// grid is x, y in 0..n-1 as one problem over ints.
func grid(n int, sense optimize.Sense, feasible func(x, y int) bool, obj func(x, y int) int64) optimize.Problem[int] {
	d := make([]int, n)
	for i := range d {
		d[i] = i
	}
	p := optimize.Problem[int]{Vars: []optimize.Var[int]{{"x", d}, {"y", d}}, Sense: sense,
		Objective: func(a []int) int64 { return obj(a[0], a[1]) }}
	if feasible != nil {
		p.Feasible = func(a []int) bool { return feasible(a[0], a[1]) }
	}
	return p
}

func TestKnapsackConformance(t *testing.T) {
	res := solve(t, context.Background(), knapsack(), optimize.Config{})
	want := optimize.Result[bool]{Status: optimize.Optimal, Assignment: []bool{true, true, false}, Value: 7,
		Stats: optimize.Stats{Evaluations: 8, Feasible: 5}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v, want %+v", res, want)
	}
}

func TestMinimize(t *testing.T) {
	// Cheapest x, y with x + y >= 4 at costs 3 and 2: (1,3), cost 9.
	p := grid(4, optimize.Minimize, func(x, y int) bool { return x+y >= 4 }, func(x, y int) int64 { return int64(3*x + 2*y) })
	res := solve(t, context.Background(), p, optimize.Config{})
	want := optimize.Result[int]{Status: optimize.Optimal, Assignment: []int{1, 3}, Value: 9,
		Stats: optimize.Stats{Evaluations: 16, Feasible: 6}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v, want %+v", res, want)
	}
}

// Equal optima are (0,2), (1,1), (2,0); the first enumerated wins, for both
// senses, and repeated runs agree.
func TestTiesGoToTheFirstEnumerated(t *testing.T) {
	sum := func(x, y int) int64 { return int64(x + y) }
	for _, p := range []optimize.Problem[int]{
		grid(3, optimize.Minimize, func(x, y int) bool { return x+y >= 2 }, sum),
		grid(3, optimize.Maximize, func(x, y int) bool { return x+y <= 2 }, sum),
	} {
		a, b := solve(t, context.Background(), p, optimize.Config{}), solve(t, context.Background(), p, optimize.Config{})
		if a.Status != optimize.Optimal || !reflect.DeepEqual(a.Assignment, []int{0, 2}) || a.Value != 2 {
			t.Errorf("%v: got %+v, want Optimal [0 2] value 2", p.Sense, a)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%v: runs differ: %+v / %+v", p.Sense, a, b)
		}
	}
}

func TestInfeasible(t *testing.T) {
	p := knapsack()
	objective := 0
	p.Feasible = func([]bool) bool { return false }
	p.Objective = func([]bool) int64 { objective++; return 0 }
	res := solve(t, context.Background(), p, optimize.Config{})
	want := optimize.Result[bool]{Status: optimize.Infeasible, Stats: optimize.Stats{Evaluations: 8}}
	if !reflect.DeepEqual(res, want) || objective != 0 {
		t.Errorf("got %+v with %d Objective calls, want %+v and none", res, objective, want)
	}
}

// The only feasible assignment is the first or the last enumerated; a nil
// Feasible makes every assignment feasible, and a problem with no variables
// has exactly one (empty) assignment.
func TestBoundaries(t *testing.T) {
	zero := func(x, y int) int64 { return 0 }
	for _, c := range []struct {
		name string
		p    optimize.Problem[int]
		want []int
	}{
		{"first", grid(3, optimize.Minimize, func(x, y int) bool { return x == 0 && y == 0 }, zero), []int{0, 0}},
		{"last", grid(3, optimize.Minimize, func(x, y int) bool { return x == 2 && y == 2 }, zero), []int{2, 2}},
		{"nil Feasible", grid(3, optimize.Maximize, nil, func(x, y int) int64 { return int64(x * y) }), []int{2, 2}},
		{"no variables", optimize.Problem[int]{Sense: optimize.Minimize, Objective: func([]int) int64 { return 0 }}, []int{}},
	} {
		res := solve(t, context.Background(), c.p, optimize.Config{})
		if res.Status != optimize.Optimal || !reflect.DeepEqual(res.Assignment, c.want) {
			t.Errorf("%s: got %+v, want Optimal %v", c.name, res, c.want)
		}
	}
}

// A search that stops before evaluating every assignment is never Optimal or
// Infeasible. A limit that is not reached changes nothing.
func TestLimitsAndInterruption(t *testing.T) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()
	expired, cancel2 := context.WithDeadline(bg, time.Unix(0, 0))
	defer cancel2()
	// OPTIMIZATION.md §8: 3 evaluations see {}, {C}, {B}: BestFound, value 5.
	if res := solve(t, bg, knapsack(), optimize.Config{MaxEvaluations: 3}); res.Status != optimize.BestFound ||
		res.Reason != optimize.EvaluationLimit || res.Value != 5 || !reflect.DeepEqual(res.Assignment, []bool{false, false, true}) ||
		res.Stats.Evaluations != 3 {
		t.Errorf("limit 3: got %+v", res)
	}
	if res := solve(t, bg, knapsack(), optimize.Config{MaxEvaluations: 8}); res.Status != optimize.Optimal || res.Value != 7 {
		t.Errorf("limit 8 = space size: got %+v, want Optimal 7", res)
	}
	never := knapsack()
	never.Feasible = func([]bool) bool { return false }
	if res := solve(t, bg, never, optimize.Config{MaxEvaluations: 7}); res.Status != optimize.NoneFound ||
		res.Reason != optimize.EvaluationLimit || res.Assignment != nil {
		t.Errorf("infeasible, limit 7: got %+v, want NoneFound", res)
	}
	for ctx, want := range map[context.Context]optimize.StopReason{canceled: optimize.Canceled, expired: optimize.DeadlineExceeded} {
		called := false
		p := knapsack()
		p.Feasible = func([]bool) bool { called = true; return true }
		if res := solve(t, ctx, p, optimize.Config{}); res.Status != optimize.NoneFound || res.Reason != want ||
			res.Stats.Evaluations != 0 || called {
			t.Errorf("%v before the first evaluation: got %+v, called %v", want, res, called)
		}
	}
	// Canceled from inside the second Objective call, observed before
	// evaluation 3 with K = 1.
	ctx, cancel3 := context.WithCancel(bg)
	defer cancel3()
	p := knapsack()
	objective := p.Objective
	calls := 0
	p.Objective = func(a []bool) int64 {
		if calls++; calls == 2 {
			cancel3()
		}
		return objective(a)
	}
	if res := solve(t, ctx, p, optimize.Config{CheckInterval: 1}); res.Status != optimize.BestFound ||
		res.Reason != optimize.Canceled || res.Stats.Evaluations != 2 || res.Value != 5 {
		t.Errorf("canceled mid-search: got %+v", res)
	}
}

func TestCallbackPanicsAreModelErrors(t *testing.T) {
	for name, mutate := range map[string]func(p *optimize.Problem[bool]){
		"Feasible":  func(p *optimize.Problem[bool]) { p.Feasible = func([]bool) bool { panic("feasible") } },
		"Objective": func(p *optimize.Problem[bool]) { p.Objective = func([]bool) int64 { panic("objective") } },
	} {
		p := knapsack()
		mutate(&p)
		res := solve(t, context.Background(), p, optimize.Config{})
		var pe *optimize.PanicError
		if res.Status != optimize.ModelError || !errors.As(res.Err, &pe) || res.Assignment != nil {
			t.Errorf("%s: got %+v", name, res)
		}
		// A panic in Feasible leaves that evaluation uncounted; one in
		// Objective comes after feasibility was decided.
		want := map[string]optimize.Stats{"Feasible": {}, "Objective": {Evaluations: 1, Feasible: 1}}[name]
		if res.Stats != want {
			t.Errorf("%s: stats %+v, want %+v", name, res.Stats, want)
		}
	}
}

// A callback that modifies its argument changes neither what the next
// callback sees nor the search.
func TestCallbacksCannotChangeTheAssignment(t *testing.T) {
	p := knapsack()
	feasible := p.Feasible
	p.Feasible = func(a []bool) bool {
		ok := feasible(a)
		a[0], a[1], a[2] = true, true, true
		return ok
	}
	res := solve(t, context.Background(), p, optimize.Config{})
	if res.Status != optimize.Optimal || res.Value != 7 || !reflect.DeepEqual(res.Assignment, []bool{true, true, false}) {
		t.Errorf("got %+v", res)
	}
}

func TestInvalidConfigRejectedBeforeCallbacks(t *testing.T) {
	called := false
	ok := knapsack()
	ok.Feasible = func([]bool) bool { called = true; return true }
	noSense, noObjective, emptyDomain := ok, ok, ok
	noSense.Sense = 0
	noObjective.Objective = nil
	emptyDomain.Vars = append([]optimize.Var[bool]{{"E", nil}}, ok.Vars...)
	for name, c := range map[string]struct {
		p   optimize.Problem[bool]
		cfg optimize.Config
	}{
		"no sense":       {noSense, optimize.Config{}},
		"no objective":   {noObjective, optimize.Config{}},
		"empty domain":   {emptyDomain, optimize.Config{}},
		"negative limit": {ok, optimize.Config{MaxEvaluations: -1}},
		"negative K":     {ok, optimize.Config{CheckInterval: -1}},
	} {
		if _, err := optimize.Solve(context.Background(), c.p, c.cfg); !errors.Is(err, optimize.ErrInvalidConfig) {
			t.Errorf("%s: err = %v, want ErrInvalidConfig", name, err)
		}
	}
	if called {
		t.Error("a callback ran for an invalid configuration")
	}
}

// reference solves p by recursive enumeration of every assignment, keeping
// the first n (all when n is 0), independently of Solve's odometer.
func reference(p optimize.Problem[int], n int64) optimize.Result[int] {
	var all [][]int
	var rec func(prefix []int)
	rec = func(prefix []int) {
		if len(prefix) == len(p.Vars) {
			all = append(all, append([]int(nil), prefix...))
			return
		}
		for _, v := range p.Vars[len(prefix)].Domain {
			rec(append(prefix, v))
		}
	}
	rec(nil)
	complete := n == 0 || n >= int64(len(all))
	if !complete {
		all = all[:n]
	}
	res := optimize.Result[int]{Stats: optimize.Stats{Evaluations: int64(len(all))}}
	for _, a := range all {
		if !p.Feasible(a) {
			continue
		}
		res.Stats.Feasible++
		v := p.Objective(a)
		if res.Assignment == nil || (p.Sense == optimize.Minimize && v < res.Value) || (p.Sense == optimize.Maximize && v > res.Value) {
			res.Assignment, res.Value = a, v
		}
	}
	switch {
	case complete && res.Assignment != nil:
		res.Status = optimize.Optimal
	case complete:
		res.Status = optimize.Infeasible
	case res.Assignment != nil:
		res.Status, res.Reason = optimize.BestFound, optimize.EvaluationLimit
	default:
		res.Status, res.Reason = optimize.NoneFound, optimize.EvaluationLimit
	}
	return res
}

// Random small problems, complete and limited, against the reference.
func TestMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 500 {
		vars := make([]optimize.Var[int], 1+rng.IntN(4))
		weights := make([]int64, len(vars))
		for j := range vars {
			vars[j].Domain = rng.Perm(1 + rng.IntN(4))
			weights[j] = rng.Int64N(7) - 3
		}
		limit := rng.Int64N(int64(rng.IntN(12) + 1)) // often 0 (no limit) or small
		density := rng.IntN(4)                       // 0: infeasible
		p := optimize.Problem[int]{Vars: vars, Sense: optimize.Sense(1 + rng.IntN(2)),
			Feasible: func(a []int) bool {
				h := 7
				for _, v := range a {
					h = h*31 + v
				}
				return h%4 < density
			},
			Objective: func(a []int) int64 {
				var s int64
				for j, v := range a {
					s += weights[j] * int64(v)
				}
				return s % 5 // few distinct values, so ties are common
			}}
		got := solve(t, context.Background(), p, optimize.Config{MaxEvaluations: limit})
		if want := reference(p, limit); !reflect.DeepEqual(got, want) {
			t.Fatalf("instance %d (limit %d):\n got %+v\nwant %+v", i, limit, got, want)
		}
	}
}
