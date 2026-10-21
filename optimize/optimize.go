// Package optimize finds a best configuration in a finite, declared space
// (Application B, docs/OPTIMIZATION.md). This version is exhaustive search
// over assignment problems (OPTIMIZATION.md §2, shape (a)): it evaluates every
// full assignment, so Optimal and Infeasible are proofs over the declared
// domains. It shares no search loop or status values with check (D-009).
package optimize

import (
	"context"
	"errors"
	"fmt"
)

// DefaultCheckInterval is the default K in the interruption schedule: the
// context is checked before evaluations 1, 1+K, 1+2K, ...
const DefaultCheckInterval = 1024

// ErrInvalidConfig is wrapped by every error Solve returns: an invalid
// Problem or Config. It is a caller error, distinct from ModelError.
var ErrInvalidConfig = errors.New("optimize: invalid configuration")

// Sense says whether the objective is minimized or maximized. The zero value
// is invalid, so a problem must state it.
type Sense int

const (
	Minimize Sense = iota + 1
	Maximize
)

// Var is a decision variable. Its values are tried in Domain order.
type Var[V any] struct {
	Name   string
	Domain []V
}

// Problem is an assignment problem: choose one value from each variable's
// domain. An assignment a has a[i] from Vars[i].Domain. The callbacks must
// be deterministic; a is valid only during the call and must not be
// retained.
type Problem[V any] struct {
	Vars  []Var[V]
	Sense Sense
	// Feasible reports whether a satisfies every constraint. nil means every
	// assignment is feasible.
	Feasible func(a []V) bool
	// Objective is the value to minimize or maximize. It is called only on
	// feasible assignments.
	Objective func(a []V) int64
}

// Config limits a search. Time limits are expressed through the context.
type Config struct {
	// MaxEvaluations ends the search once this many assignments have been
	// evaluated and at least one remains. 0 means no limit.
	MaxEvaluations int64
	// CheckInterval is K in the interruption schedule. 0 means
	// DefaultCheckInterval. Tests that need deterministic interruption use 1.
	CheckInterval int
}

// Solve enumerates every assignment of p in lexicographic order of domain
// positions (Vars[0] varies slowest) and returns the best feasible one. Among
// assignments with the best value, the first enumerated is returned. It
// returns a non-nil error only for an invalid problem or configuration
// (wrapping ErrInvalidConfig), in which case no callback runs. All other
// outcomes, including callback panics, are reported in Result.Status.
func Solve[V any](ctx context.Context, p Problem[V], cfg Config) (Result[V], error) {
	if err := validate(p, cfg); err != nil {
		return Result[V]{}, err
	}
	k := int64(cfg.CheckInterval)
	if k == 0 {
		k = DefaultCheckInterval
	}
	idx := make([]int, len(p.Vars)) // the current assignment, as domain positions
	a := make([]V, len(p.Vars))
	fill := func() { // refilled before each callback, so a callback cannot change what the next one sees
		for i, j := range idx {
			a[i] = p.Vars[i].Domain[j]
		}
	}
	var (
		res   Result[V]
		best  []int
		found bool
	)
	for {
		if res.Stats.Evaluations%k == 0 {
			if r, stop := interrupted(ctx); stop {
				return stopped(res, p, best, found, r), nil
			}
		}
		if cfg.MaxEvaluations > 0 && res.Stats.Evaluations == cfg.MaxEvaluations {
			return stopped(res, p, best, found, EvaluationLimit), nil
		}
		feasible := true
		if p.Feasible != nil {
			if err := call(func() { fill(); feasible = p.Feasible(a) }); err != nil {
				return Result[V]{Status: ModelError, Err: err, Stats: res.Stats}, nil
			}
		}
		res.Stats.Evaluations++
		if feasible {
			res.Stats.Feasible++
			var v int64
			if err := call(func() { fill(); v = p.Objective(a) }); err != nil {
				return Result[V]{Status: ModelError, Err: err, Stats: res.Stats}, nil
			}
			if !found || better(p.Sense, v, res.Value) {
				best, res.Value, found = append(best[:0], idx...), v, true
			}
		}
		if !advance(idx, p.Vars) {
			break
		}
	}
	res.Status = Infeasible
	if found {
		res.Status, res.Assignment = Optimal, values(p, best)
	}
	return res, nil
}

func validate[V any](p Problem[V], cfg Config) error {
	if p.Sense != Minimize && p.Sense != Maximize {
		return fmt.Errorf("%w: Sense is %d, must be Minimize or Maximize", ErrInvalidConfig, p.Sense)
	}
	if p.Objective == nil {
		return fmt.Errorf("%w: Objective is nil", ErrInvalidConfig)
	}
	for i, v := range p.Vars {
		if len(v.Domain) == 0 {
			return fmt.Errorf("%w: variable %d (%q) has an empty domain", ErrInvalidConfig, i, v.Name)
		}
	}
	if cfg.MaxEvaluations < 0 {
		return fmt.Errorf("%w: MaxEvaluations is %d, must be >= 0", ErrInvalidConfig, cfg.MaxEvaluations)
	}
	if cfg.CheckInterval < 0 {
		return fmt.Errorf("%w: CheckInterval is %d, must be >= 0", ErrInvalidConfig, cfg.CheckInterval)
	}
	return nil
}

// better reports whether v strictly improves on the incumbent. Strictness
// keeps the first optimum enumerated. Maximization compares directly rather
// than negating, which would overflow at math.MinInt64.
func better(s Sense, v, incumbent int64) bool {
	if s == Minimize {
		return v < incumbent
	}
	return v > incumbent
}

// advance moves idx to the next assignment, the last variable fastest. It
// returns false after the last one.
func advance[V any](idx []int, vars []Var[V]) bool {
	for i := len(idx) - 1; i >= 0; i-- {
		if idx[i]++; idx[i] < len(vars[i].Domain) {
			return true
		}
		idx[i] = 0
	}
	return false
}

func values[V any](p Problem[V], idx []int) []V {
	a := make([]V, len(idx))
	for i, j := range idx {
		a[i] = p.Vars[i].Domain[j]
	}
	return a
}

// stopped builds the result of a search ended by a limit or an interruption.
// It is never Optimal or Infeasible: some assignments were not evaluated.
func stopped[V any](res Result[V], p Problem[V], best []int, found bool, r StopReason) Result[V] {
	res.Status, res.Reason = NoneFound, r
	if found {
		res.Status, res.Assignment = BestFound, values(p, best)
	}
	return res
}

// call runs f, turning a panic into a *PanicError. f is a callback plus the
// fill that precedes it; fill only indexes valid domain positions, so in
// practice a recovered panic comes from the callback.
func call(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r}
		}
	}()
	f()
	return nil
}

func interrupted(ctx context.Context) (StopReason, bool) {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return DeadlineExceeded, true
		}
		return Canceled, true
	}
	return NoReason, false
}
