// Package tracetest checks counterexamples in regression tests. It is
// internal: a test convenience, not part of the Atlas API.
package tracetest

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
)

// Expect fails t unless res is a Violation of inv whose trace takes exactly
// the given actions, compared by their fmt "%v" text, one per step after the
// initial state. It also replays the trace against m without the engine:
// step 0 must be an Init emission, every later step an emission of the
// previous state's Next with the same action text and key, every state before
// the last must satisfy inv, and the last must not.
func Expect[S, A any](t testing.TB, m core.Model[S, A], inv check.Invariant[S], res check.Result[S, A], actions ...string) {
	t.Helper()
	if res.Status != check.Violation || res.Violation == nil {
		t.Fatalf("status = %v, want Violation of %q\n%v", res.Status, inv.Name, res)
	}
	if res.Violation.Invariant != inv.Name {
		t.Fatalf("violated invariant = %q, want %q", res.Violation.Invariant, inv.Name)
	}
	steps := res.Violation.Trace.Steps
	got := make([]string, 0, len(steps))
	for _, st := range steps[1:] {
		got = append(got, fmt.Sprint(st.Action))
	}
	if !slices.Equal(got, actions) {
		t.Fatalf("trace actions (%d steps):\n  %q\nwant (%d steps):\n  %q", len(got), got, len(actions), actions)
	}

	key := func(s S) string { return string(m.AppendKey(nil, s)) }
	found := false
	m.Init(func(s S) bool { found = found || key(s) == key(steps[0].State); return true })
	if !found {
		t.Fatalf("step 0 is not an initial state: %+v", steps[0].State)
	}
	for i := 1; i < len(steps); i++ {
		found = false
		want := fmt.Sprint(steps[i].Action)
		m.Next(steps[i-1].State, func(a A, s S) bool {
			found = found || (fmt.Sprint(a) == want && key(s) == key(steps[i].State))
			return true
		})
		if !found {
			t.Fatalf("step %d (%s) is not a successor of step %d", i, want, i-1)
		}
	}
	for i, st := range steps {
		if last := i == len(steps)-1; inv.Holds(st.State) == last {
			t.Fatalf("step %d: %q holds = %v, want %v", i, inv.Name, last, !last)
		}
	}
}
