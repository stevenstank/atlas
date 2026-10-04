package check

import (
	"fmt"
	"slices"

	"github.com/stevenstank/atlas/core"
)

// trace rebuilds the path to id (D-005). It replays Init and Next, taking
// the emission with each stored edge ordinal, and checks every replayed
// state's key against the store. A mismatch means the model is
// nondeterministic.
func (e *engine[S, A]) trace(id core.StateID) (Trace[S, A], error) {
	var path []core.StateID
	for x := id; x != core.NoParent; x = e.store.Parent(x) {
		path = append(path, x)
	}
	slices.Reverse(path)

	steps := make([]Step[S, A], 0, len(path))
	var cur S
	for i, x := range path {
		want := e.store.Edge(x)
		var (
			act   A
			st    S
			n     uint32
			found bool
			bad   bool // emitted again after we returned false
		)
		pick := func(a A, s S) bool {
			if found {
				bad = true
				return false
			}
			if n == want {
				act, st, found = a, s, true
				return false
			}
			n++
			return true
		}
		// take is engine code called from inside the model. Like emitInit, it
		// clears inModel and restores it without defer, so an engine panic in
		// pick is re-raised by run instead of being reported as a ModelError.
		take := func(a A, s S) bool {
			e.inModel = false
			ok := pick(a, s)
			e.inModel = true
			return ok
		}
		if i == 0 {
			e.callModel(func() { e.m.Init(func(s S) bool { var zero A; return take(zero, s) }) })
		} else {
			e.callModel(func() { e.m.Next(cur, take) })
		}
		switch {
		case bad:
			return Trace[S, A]{}, fmt.Errorf("%w (during trace replay, step %d)", ErrCallbackContract, i)
		case !found:
			return Trace[S, A]{}, fmt.Errorf("%w: step %d: emission %d no longer exists", ErrNondeterministic, i, want)
		case string(e.key(st)) != e.store.Key(x):
			return Trace[S, A]{}, fmt.Errorf("%w: step %d: replayed state has a different key", ErrNondeterministic, i)
		}
		steps = append(steps, Step[S, A]{Action: act, State: st})
		cur = st
	}
	return Trace[S, A]{Steps: steps}, nil
}
