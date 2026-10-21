package optimize_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/stevenstank/atlas/optimize"
)

// items is a 0/1 knapsack with Prune (over capacity) and Bound (the value
// so far plus every remaining item), both valid. Domains are exclude before
// include, as in OPTIMIZATION.md §8.
func items(weight, value []int64, capacity int64) optimize.Problem[bool] {
	sum := func(a []bool, w []int64) (s int64) {
		for i, in := range a {
			if in {
				s += w[i]
			}
		}
		return s
	}
	vars := make([]optimize.Var[bool], len(weight))
	for i := range vars {
		vars[i] = optimize.Var[bool]{Domain: []bool{false, true}}
	}
	return optimize.Problem[bool]{
		Vars:      vars,
		Sense:     optimize.Maximize,
		Feasible:  func(a []bool) bool { return sum(a, weight) <= capacity },
		Objective: func(a []bool) int64 { return sum(a, value) },
		Prune:     func(p []bool) bool { return sum(p, weight) > capacity },
		Bound: func(p []bool) int64 {
			b := sum(p, value)
			for _, v := range value[len(p):] {
				b += v
			}
			return b
		},
	}
}

// exhaustive is p without Prune and Bound.
func exhaustive[V any](p optimize.Problem[V]) optimize.Problem[V] {
	p.Prune, p.Bound = nil, nil
	return p
}

// sameAnswer reports whether a and b agree on everything but statistics.
func sameAnswer[V any](a, b optimize.Result[V]) bool {
	a.Stats, b.Stats = optimize.Stats{}, optimize.Stats{}
	return reflect.DeepEqual(a, b)
}

func TestBranchAndBoundKnapsack(t *testing.T) {
	bg := context.Background()
	small := items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5) // OPTIMIZATION.md §8
	res := solve(t, bg, small, optimize.Config{})
	if res.Status != optimize.Optimal || res.Value != 7 || !reflect.DeepEqual(res.Assignment, []bool{true, true, false}) {
		t.Errorf("§8 knapsack: got %+v, want Optimal {A,B} 7", res)
	}

	// 14 items: the cuts must leave far fewer than 2^14 evaluations, with
	// the same answer as exhaustive search.
	w := []int64{12, 7, 11, 8, 9, 6, 5, 14, 3, 10, 4, 13, 2, 9}
	v := []int64{24, 13, 23, 15, 16, 11, 9, 28, 5, 20, 7, 25, 3, 17}
	big := items(w, v, 40)
	got, want := solve(t, bg, big, optimize.Config{}), solve(t, bg, exhaustive(big), optimize.Config{})
	if !sameAnswer(got, want) || want.Stats.Evaluations != 1<<14 {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.Stats.Evaluations >= 1<<14/4 || got.Stats.Pruned == 0 || got.Stats.Bounded == 0 {
		t.Errorf("cuts saved too little: %+v", got.Stats)
	}
}

func TestBranchAndBoundMinimize(t *testing.T) {
	// Cheapest x, y with x + y >= 4 at costs 3 and 2: (1,3), cost 9. Bound
	// is the cost so far; Prune rejects an x that y cannot make up for.
	p := grid(4, optimize.Minimize, func(x, y int) bool { return x+y >= 4 }, func(x, y int) int64 { return int64(3*x + 2*y) })
	p.Bound = func(a []int) int64 {
		if len(a) == 0 {
			return 0
		}
		return int64(3 * a[0])
	}
	p.Prune = func(a []int) bool { return len(a) == 1 && a[0]+3 < 4 }
	res := solve(t, context.Background(), p, optimize.Config{})
	want := optimize.Result[int]{Status: optimize.Optimal, Assignment: []int{1, 3}, Value: 9,
		// Root: Prune only. x=0 pruned. x=1: 4 evaluations, (1,3) feasible,
		// cost 9. x=2: bound 6 < 9, explored, (2,2) and (2,3) feasible.
		// x=3: bound 9, cut.
		Stats: optimize.Stats{Evaluations: 8, Feasible: 3, Nodes: 5, Pruned: 1, Bounded: 1}}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v, want %+v", res, want)
	}
}

// A tight Bound cuts every subtree whose best equals the incumbent, and the
// first optimum enumerated still wins, for both senses.
func TestBranchAndBoundTies(t *testing.T) {
	sum := func(x, y int) int64 { return int64(x + y) }
	for _, p := range []optimize.Problem[int]{
		grid(3, optimize.Minimize, func(x, y int) bool { return x+y >= 2 }, sum),
		grid(3, optimize.Maximize, func(x, y int) bool { return x+y <= 2 }, sum),
	} {
		p.Bound = tight(p)
		got := solve(t, context.Background(), p, optimize.Config{VerifyPruning: true})
		if want := solve(t, context.Background(), exhaustive(p), optimize.Config{}); !sameAnswer(got, want) ||
			!reflect.DeepEqual(got.Assignment, []int{0, 2}) || got.Stats.Bounded == 0 {
			t.Errorf("%v: got %+v, want %+v with cuts", p.Sense, got, want)
		}
	}
}

func TestBranchAndBoundInfeasibleAndBoundaries(t *testing.T) {
	bg := context.Background()
	// Prune at the root proves Infeasible without an evaluation.
	p := knapsack()
	p.Feasible = func([]bool) bool { return false }
	p.Prune = func(a []bool) bool { return true }
	want := optimize.Result[bool]{Status: optimize.Infeasible, Stats: optimize.Stats{Nodes: 1, Pruned: 1}}
	if res := solve(t, bg, p, optimize.Config{}); !reflect.DeepEqual(res, want) {
		t.Errorf("pruned root: got %+v, want %+v", res, want)
	}
	// Prune that never cuts, and a Bound with no incumbent to beat, leave
	// an infeasible problem fully evaluated; Bound is never called.
	p.Prune = func([]bool) bool { return false }
	p.Bound = func([]bool) int64 { panic("Bound called without an incumbent") }
	want = optimize.Result[bool]{Status: optimize.Infeasible, Stats: optimize.Stats{Evaluations: 8, Nodes: 7}}
	if res := solve(t, bg, p, optimize.Config{}); !reflect.DeepEqual(res, want) {
		t.Errorf("no cuts: got %+v, want %+v", res, want)
	}
	// The only feasible assignment is the last one enumerated.
	last := grid(3, optimize.Minimize, func(x, y int) bool { return x == 2 && y == 2 }, func(x, y int) int64 { return 0 })
	last.Prune = func(a []int) bool { return len(a) == 1 && a[0] != 2 }
	if res := solve(t, bg, last, optimize.Config{}); res.Status != optimize.Optimal || !reflect.DeepEqual(res.Assignment, []int{2, 2}) ||
		res.Stats.Evaluations != 3 {
		t.Errorf("last: got %+v", res)
	}
	// No variables: no partial assignment exists, so neither is called.
	none := optimize.Problem[int]{Sense: optimize.Minimize, Objective: func([]int) int64 { return 0 },
		Prune: func([]int) bool { panic("Prune called") }}
	if res := solve(t, bg, none, optimize.Config{}); res.Status != optimize.Optimal || res.Stats.Nodes != 0 {
		t.Errorf("no variables: got %+v", res)
	}
}

// A stopped branch-and-bound search is never Optimal or Infeasible, and a
// limit that cuts make unreachable changes nothing.
func TestBranchAndBoundLimitsAndInterruption(t *testing.T) {
	bg := context.Background()
	p := items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5)
	full := solve(t, bg, p, optimize.Config{})
	if res := solve(t, bg, p, optimize.Config{MaxEvaluations: full.Stats.Evaluations}); !reflect.DeepEqual(res, full) {
		t.Errorf("limit = evaluations needed: got %+v, want %+v", res, full)
	}
	for limit := int64(1); limit < full.Stats.Evaluations; limit++ {
		if res := solve(t, bg, p, optimize.Config{MaxEvaluations: limit}); res.Status != optimize.BestFound ||
			res.Reason != optimize.EvaluationLimit || res.Stats.Evaluations != limit {
			t.Errorf("limit %d: got %+v", limit, res)
		}
	}
	// Canceled inside Prune: observed before the next step with K = 1, so
	// at a node, before any further evaluation.
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	prune := p.Prune
	calls := 0
	p.Prune = func(a []bool) bool {
		if calls++; calls == 3 {
			cancel()
		}
		return prune(a)
	}
	res := solve(t, ctx, p, optimize.Config{CheckInterval: 1})
	if res.Status != optimize.NoneFound || res.Reason != optimize.Canceled || res.Stats.Nodes != 3 || res.Stats.Evaluations != 0 {
		t.Errorf("canceled in Prune: got %+v", res)
	}
	// Canceled inside the c-th Feasible call, for every c but the last:
	// with VerifyPruning, these include calls made while verifying a cut.
	q := items([]int64{2, 3, 4, 1, 5}, []int64{3, 4, 5, 1, 6}, 7)
	whole := solve(t, bg, q, optimize.Config{VerifyPruning: true})
	if whole.Stats.Verified == 0 {
		t.Fatalf("no verification to interrupt: %+v", whole)
	}
	feasible := q.Feasible
	for c := 1; c < int(whole.Stats.Evaluations+whole.Stats.Verified); c++ {
		ctx, cancel := context.WithCancel(bg)
		calls := 0
		q.Feasible = func(a []bool) bool {
			if calls++; calls == c {
				cancel()
			}
			return feasible(a)
		}
		res := solve(t, ctx, q, optimize.Config{CheckInterval: 1, VerifyPruning: true})
		cancel()
		if (res.Status != optimize.BestFound && res.Status != optimize.NoneFound) || res.Reason != optimize.Canceled {
			t.Errorf("canceled in Feasible call %d: got %+v", c, res)
		}
	}
}

func TestPruneAndBoundPanicsAreModelErrors(t *testing.T) {
	for name, mutate := range map[string]func(p *optimize.Problem[bool]){
		"Prune": func(p *optimize.Problem[bool]) { p.Prune = func([]bool) bool { panic("prune") } },
		"Bound": func(p *optimize.Problem[bool]) { p.Bound = func([]bool) int64 { panic("bound") } },
	} {
		p := items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5)
		mutate(&p)
		res := solve(t, context.Background(), p, optimize.Config{})
		var pe *optimize.PanicError
		if res.Status != optimize.ModelError || !errors.As(res.Err, &pe) || res.Assignment != nil {
			t.Errorf("%s: got %+v", name, res)
		}
	}
}

// VerifyPruning catches a cut that breaks its contract; a broken callback
// that never causes a cut is not caught.
func TestVerifyPruning(t *testing.T) {
	bg := context.Background()
	p := items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5)
	p.Bound = func([]bool) int64 { return 0 } // claims nothing beats 0
	if res := solve(t, bg, p, optimize.Config{}); res.Status != optimize.Optimal || res.Value == 7 {
		t.Fatalf("unverified invalid Bound: got %+v; this test expects a wrong Optimal", res)
	}
	res := solve(t, bg, p, optimize.Config{VerifyPruning: true})
	if res.Status != optimize.ModelError || !errors.Is(res.Err, optimize.ErrInvalidBound) || res.Assignment != nil {
		t.Errorf("invalid Bound: got %+v", res)
	}

	p = items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5)
	p.Prune = func(a []bool) bool { return len(a) == 1 && a[0] } // cuts every set with A
	res = solve(t, bg, p, optimize.Config{VerifyPruning: true})
	if res.Status != optimize.ModelError || !errors.Is(res.Err, optimize.ErrInvalidPrune) {
		t.Errorf("invalid Prune: got %+v", res)
	}

	// Bound too low for every partial with A, but the search never asks
	// Bound there before the optimum is found, and afterwards the cuts it
	// causes are still right.
	p = items([]int64{2, 3, 4}, []int64{3, 4, 5}, 5)
	bound := p.Bound
	p.Bound = func(a []bool) int64 {
		if len(a) == 0 {
			return -1 // wrong, but never asked: the root has no incumbent
		}
		return bound(a)
	}
	if res := solve(t, bg, p, optimize.Config{VerifyPruning: true}); res.Status != optimize.Optimal || res.Value != 7 {
		t.Errorf("never-asked wrong Bound: got %+v", res)
	}

	// With valid callbacks, every assignment is either evaluated or
	// verified, exactly once.
	p = items([]int64{2, 3, 4, 1, 5}, []int64{3, 4, 5, 1, 6}, 7)
	res = solve(t, bg, p, optimize.Config{VerifyPruning: true})
	if res.Status != optimize.Optimal || res.Stats.Evaluations+res.Stats.Verified != 32 || res.Stats.Verified == 0 {
		t.Errorf("valid callbacks: got %+v", res)
	}
}

// tight returns the exact best objective over the feasible completions of a
// partial assignment, or the worst int64 if there are none: the strongest
// valid Bound.
func tight(p optimize.Problem[int]) func([]int) int64 {
	return func(partial []int) int64 {
		best, found := int64(0), false
		completions(p, partial, func(a []int) {
			if p.Feasible != nil && !p.Feasible(a) {
				return
			}
			v := p.Objective(a)
			if !found || (p.Sense == optimize.Minimize && v < best) || (p.Sense == optimize.Maximize && v > best) {
				best, found = v, true
			}
		})
		if !found {
			if p.Sense == optimize.Minimize {
				return 1<<63 - 1
			}
			return -1 << 63
		}
		return best
	}
}

func completions(p optimize.Problem[int], partial []int, f func([]int)) {
	a := append([]int(nil), partial...)
	var rec func()
	rec = func() {
		if len(a) == len(p.Vars) {
			f(a)
			return
		}
		for _, v := range p.Vars[len(a)].Domain {
			a = append(a, v)
			rec()
			a = a[:len(a)-1]
		}
	}
	rec()
}

func hash(seed int, a []int) int {
	h := seed
	for _, v := range a {
		h = h*31 + v + 1
	}
	if h < 0 {
		h = -h
	}
	return h
}

// Random small problems: branch-and-bound with valid Prune and Bound (some
// loosened, some missing cuts) gives exhaustive search's answer; with
// deliberately broken ones and VerifyPruning, it gives that answer or a
// ModelError, never a different answer.
func TestBranchAndBoundMatchesExhaustive(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	bg := context.Background()
	var cut, caught, survived int // instances where valid callbacks cut; broken ones were caught; or not
	for i := range 500 {
		vars := make([]optimize.Var[int], 1+rng.IntN(4))
		weights := make([]int64, len(vars))
		total := int64(1)
		for j := range vars {
			vars[j].Domain = rng.Perm(1 + rng.IntN(4))
			weights[j] = rng.Int64N(7) - 3
			total *= int64(len(vars[j].Domain))
		}
		density, seed := rng.IntN(4), rng.IntN(1000)
		p := optimize.Problem[int]{Vars: vars, Sense: optimize.Sense(1 + rng.IntN(2)),
			Feasible: func(a []int) bool { return hash(7, a)%4 < density },
			Objective: func(a []int) int64 {
				var s int64
				for j, v := range a {
					s += weights[j] * int64(v)
				}
				return s % 5
			}}
		want := solve(t, bg, p, optimize.Config{})
		exact := tight(p)
		worst := map[optimize.Sense]int64{optimize.Minimize: 1<<63 - 1, optimize.Maximize: -1 << 63}[p.Sense]
		slack := map[optimize.Sense]int64{optimize.Minimize: -1, optimize.Maximize: 1}[p.Sense]

		valid := p
		valid.Prune = func(a []int) bool { // only partials with no feasible completion, some of them
			return exact(a) == worst && hash(seed, a)%3 != 0
		}
		valid.Bound = func(a []int) int64 { // the exact bound, sometimes loosened
			if b := exact(a); b != worst {
				return b + slack*int64(hash(seed+1, a)%3)
			}
			return exact(a)
		}
		got := solve(t, bg, valid, optimize.Config{VerifyPruning: true})
		if !sameAnswer(got, want) || got.Stats.Evaluations+got.Stats.Verified != total {
			t.Fatalf("instance %d, valid callbacks:\n got %+v\nwant %+v (%d assignments)", i, got, want, total)
		}
		if got.Stats.Pruned+got.Stats.Bounded > 0 {
			cut++
		}

		broken := valid
		broken.Prune = func(a []int) bool { return hash(seed+2, a)%5 == 0 }
		broken.Bound = func(a []int) int64 { return exact(a) - slack*int64(hash(seed+3, a)%3) }
		got = solve(t, bg, broken, optimize.Config{VerifyPruning: true})
		if got.Status == optimize.ModelError {
			if !errors.Is(got.Err, optimize.ErrInvalidPrune) && !errors.Is(got.Err, optimize.ErrInvalidBound) {
				t.Fatalf("instance %d, broken callbacks: unexpected error %v", i, got.Err)
			}
			caught++
		} else if !sameAnswer(got, want) {
			t.Fatalf("instance %d, broken callbacks, verified:\n got %+v\nwant %+v", i, got, want)
		} else {
			survived++
		}
	}
	t.Logf("valid callbacks cut in %d instances; broken ones caught in %d, harmless in %d", cut, caught, survived)
	if cut < 100 || caught < 100 || survived < 100 {
		t.Errorf("too few instances exercise each case: cut %d, caught %d, harmless %d", cut, caught, survived)
	}
}
