// Package optimize finds a best configuration in a finite, declared space
// (Application B, docs/OPTIMIZATION.md). This version searches assignment
// problems (OPTIMIZATION.md §2, shape (a)) depth-first: exhaustively, or by
// branch-and-bound when the problem supplies Prune or Bound (§5). Optimal and
// Infeasible are proofs over the declared domains, provided Prune and Bound
// keep their contracts. It shares no search loop or status values with check
// (D-009).
package optimize

import (
	"context"
	"errors"
	"fmt"
)

// DefaultCheckInterval is the default K in the interruption schedule: the
// context is checked before search steps 1, 1+K, 1+2K, ..., where a step is
// an evaluation, a node (Stats.Nodes), or a verification evaluation. Without
// Prune, Bound, and VerifyPruning, the steps are the evaluations.
const DefaultCheckInterval = 1024

// ErrInvalidConfig is wrapped by every error Solve returns: an invalid
// Problem or Config. It is a caller error, distinct from ModelError.
var ErrInvalidConfig = errors.New("optimize: invalid configuration")

// ErrInvalidPrune and ErrInvalidBound are wrapped by Result.Err when
// Config.VerifyPruning finds that a cut broke the Prune or Bound contract.
var (
	ErrInvalidPrune = errors.New("optimize: Prune cut a subtree with a feasible completion")
	ErrInvalidBound = errors.New("optimize: Bound cut a subtree with a better feasible completion")
)

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

	// Prune and Bound are optional, and make the search branch-and-bound.
	// Both are called on partial assignments: partial holds values for
	// Vars[0] … Vars[len(partial)-1], with 0 <= len(partial) < len(Vars),
	// and a completion of partial is any full assignment that extends it.
	// Atlas cannot check their contracts in general; a callback that breaks
	// its contract can make the result wrong (see Config.VerifyPruning).

	// Prune reports true only if no completion of partial is feasible. The
	// subtree below partial is then cut.
	Prune func(partial []V) bool
	// Bound is an optimistic bound: for Minimize, Bound(partial) <=
	// Objective(c) for every feasible completion c; for Maximize, >=. It is
	// called only once a feasible assignment has been found, and the
	// subtree is cut when the bound is not strictly better than that
	// incumbent's value.
	Bound func(partial []V) int64
}

// Config limits a search. Time limits are expressed through the context.
type Config struct {
	// MaxEvaluations ends the search once this many assignments have been
	// evaluated and another is about to be. 0 means no limit.
	MaxEvaluations int64
	// CheckInterval is K in the interruption schedule. 0 means
	// DefaultCheckInterval. Tests that need deterministic interruption use 1.
	CheckInterval int
	// VerifyPruning evaluates every completion of every cut subtree and
	// reports ModelError, wrapping ErrInvalidPrune or ErrInvalidBound, if
	// a cut broke its callback's contract. Every cut the search makes is
	// checked, but costs as much as not cutting it: this is a debug mode
	// for small instances. Bound values that did not cause a cut are not
	// checked, nor is determinism. Verification evaluations do not count
	// toward MaxEvaluations.
	VerifyPruning bool
}

// Solve searches the assignments of p depth-first, in lexicographic order of
// domain positions (Vars[0] varies slowest), and returns the best feasible
// one. Among assignments with the best value, the first enumerated is
// returned. Without Prune and Bound it evaluates every assignment. With valid
// Prune and Bound callbacks it returns the same result, including the same
// assignment, after cutting the subtrees they rule out (OPTIMIZATION.md §5).
// It returns a non-nil error only for an invalid problem or configuration
// (wrapping ErrInvalidConfig), in which case no callback runs. All other
// outcomes, including callback panics, are reported in Result.Status.
func Solve[V any](ctx context.Context, p Problem[V], cfg Config) (Result[V], error) {
	if err := validate(p, cfg); err != nil {
		return Result[V]{}, err
	}
	s := &search[V]{ctx: ctx, p: p, cfg: cfg, k: int64(cfg.CheckInterval),
		idx: make([]int, len(p.Vars)), a: make([]V, len(p.Vars))}
	if s.k == 0 {
		s.k = DefaultCheckInterval
	}
	done := s.dfs(0)
	switch {
	case s.err != nil:
		return Result[V]{Status: ModelError, Err: s.err, Stats: s.res.Stats}, nil
	case !done:
		return stopped(s.res, p, s.best, s.found, s.reason), nil
	}
	res := s.res
	res.Status = Infeasible
	if s.found {
		res.Status, res.Assignment = Optimal, values(p, s.best)
	}
	return res, nil
}

// search is the state of one Solve call.
type search[V any] struct {
	ctx   context.Context
	p     Problem[V]
	cfg   Config
	k     int64
	idx   []int // the current (partial) assignment, as domain positions
	a     []V   // refilled from idx before each callback
	res   Result[V]
	best  []int
	found bool
	steps int64 // steps begun, for the interruption schedule

	reason StopReason // why the search stopped early, when it did
	err    error      // the ModelError cause, when there is one
}

// dfs searches the subtree below the first d positions of idx. It returns
// false if the search must end: s.err or s.reason then says why.
func (s *search[V]) dfs(d int) bool {
	if d == len(s.idx) {
		return s.leaf()
	}
	if s.p.Prune != nil || (s.p.Bound != nil && s.found) {
		cut, ok := s.node(d)
		if !ok || cut {
			return ok
		}
	}
	for j := range s.p.Vars[d].Domain {
		s.idx[d] = j
		if !s.dfs(d + 1) {
			return false
		}
	}
	return true
}

// node asks Prune, then Bound, whether the subtree below the first d
// positions of idx can be cut, and verifies a cut if cfg.VerifyPruning is
// set. Bound is asked only when there is an incumbent to compare against.
func (s *search[V]) node(d int) (cut, ok bool) {
	if !s.step() {
		return false, false
	}
	pruned, bounded, bound := false, false, int64(0)
	if s.p.Prune != nil {
		if s.err = s.call(d, func(a []V) { pruned = s.p.Prune(a) }); s.err != nil {
			return false, false
		}
	}
	if !pruned && s.p.Bound != nil && s.found {
		if s.err = s.call(d, func(a []V) { bound = s.p.Bound(a) }); s.err != nil {
			return false, false
		}
		// Every feasible completion is at best bound, so none improves
		// strictly on the incumbent, and only a strict improvement would
		// replace it.
		bounded = !better(s.p.Sense, bound, s.res.Value)
	}
	s.res.Stats.Nodes++
	switch {
	case pruned:
		s.res.Stats.Pruned++
	case bounded:
		s.res.Stats.Bounded++
	default:
		return false, true
	}
	if s.cfg.VerifyPruning && !s.verify(d, d, pruned, bound) {
		return false, false
	}
	return true, true
}

// leaf evaluates the full assignment in idx.
func (s *search[V]) leaf() bool {
	if !s.step() {
		return false
	}
	if s.cfg.MaxEvaluations > 0 && s.res.Stats.Evaluations == s.cfg.MaxEvaluations {
		s.reason = EvaluationLimit
		return false
	}
	n := len(s.idx)
	feasible := true
	if s.p.Feasible != nil {
		if s.err = s.call(n, func(a []V) { feasible = s.p.Feasible(a) }); s.err != nil {
			return false
		}
	}
	s.res.Stats.Evaluations++
	if !feasible {
		return true
	}
	s.res.Stats.Feasible++
	var v int64
	if s.err = s.call(n, func(a []V) { v = s.p.Objective(a) }); s.err != nil {
		return false
	}
	if !s.found || better(s.p.Sense, v, s.res.Value) {
		s.best, s.res.Value, s.found = append(s.best[:0], s.idx...), v, true
	}
	return true
}

// verify evaluates every completion of the first d positions of idx, which
// a cut at depth cutDepth claimed to rule out: by Prune when pruned, else by
// Bound returning bound. A completion that is feasible (Prune) or strictly
// better than bound (Bound) violates the claim and is a ModelError. These
// evaluations count as Verified, not Evaluations.
func (s *search[V]) verify(d, cutDepth int, pruned bool, bound int64) bool {
	if d < len(s.idx) {
		for j := range s.p.Vars[d].Domain {
			s.idx[d] = j
			if !s.verify(d+1, cutDepth, pruned, bound) {
				return false
			}
		}
		return true
	}
	if !s.step() {
		return false
	}
	n := len(s.idx)
	feasible := true
	if s.p.Feasible != nil {
		if s.err = s.call(n, func(a []V) { feasible = s.p.Feasible(a) }); s.err != nil {
			return false
		}
	}
	s.res.Stats.Verified++
	if !feasible {
		return true
	}
	if pruned {
		s.err = fmt.Errorf("%w: Prune cut %v, but completion %v is feasible",
			ErrInvalidPrune, values(s.p, s.idx[:cutDepth]), values(s.p, s.idx))
		return false
	}
	var v int64
	if s.err = s.call(n, func(a []V) { v = s.p.Objective(a) }); s.err != nil {
		return false
	}
	if better(s.p.Sense, v, bound) {
		s.err = fmt.Errorf("%w: Bound(%v) = %d, but feasible completion %v has objective %d",
			ErrInvalidBound, values(s.p, s.idx[:cutDepth]), bound, values(s.p, s.idx), v)
		return false
	}
	return true
}

// step begins a search step (a node, an evaluation, or a verification
// evaluation), checking the context before steps 1, 1+K, 1+2K, ... It
// returns false if the context has ended.
func (s *search[V]) step() bool {
	if s.steps%s.k == 0 {
		if r, stop := interrupted(s.ctx); stop {
			s.reason = r
			return false
		}
	}
	s.steps++
	return true
}

// call runs f on the first d values of the current assignment, refilled from
// idx so that an earlier callback cannot change what f sees, turning a panic
// into a *PanicError. The refill only indexes valid domain positions, so in
// practice a recovered panic comes from the callback.
func (s *search[V]) call(d int, f func(a []V)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{Value: r}
		}
	}()
	for i, j := range s.idx[:d] {
		s.a[i] = s.p.Vars[i].Domain[j]
	}
	f(s.a[:d])
	return nil
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

func values[V any](p Problem[V], idx []int) []V {
	a := make([]V, len(idx))
	for i, j := range idx {
		a[i] = p.Vars[i].Domain[j]
	}
	return a
}

// stopped builds the result of a search ended by a limit or an interruption.
// It is never Optimal or Infeasible: some assignments were neither evaluated
// nor cut.
func stopped[V any](res Result[V], p Problem[V], best []int, found bool, r StopReason) Result[V] {
	res.Status, res.Reason = NoneFound, r
	if found {
		res.Status, res.Assignment = BestFound, values(p, best)
	}
	return res
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
