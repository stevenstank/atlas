package check

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/models"
)

// keyInjective is the TESTING.md §3 check of the model's key obligation. It
// explores reachable states by value (their %v form), not by key, then
// requires that any two states with equal keys have equal successor keys, in
// order, and equal invariant results.
func keyInjective[S, A any](m core.Model[S, A], holds func(S) bool) error {
	key := func(s S) string { return string(m.AppendKey(nil, s)) }
	succ := func(s S) []string {
		var out []string
		m.Next(s, func(_ A, t S) bool { out = append(out, key(t)); return true })
		return out
	}
	seen := map[string]S{}
	var queue []S
	visit := func(s S) bool {
		if v := fmt.Sprint(s); !hasKey(seen, v) {
			seen[v] = s
			queue = append(queue, s)
		}
		return true
	}
	m.Init(visit)
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		m.Next(s, func(_ A, t S) bool { return visit(t) })
	}
	byKey := map[string]S{}
	for _, s := range seen {
		k := key(s)
		o, ok := byKey[k]
		if !ok {
			byKey[k] = s
			continue
		}
		if !slices.Equal(succ(s), succ(o)) || (holds != nil && holds(s) != holds(o)) {
			return fmt.Errorf("states %v and %v share key %q but behave differently", s, o, k)
		}
	}
	return nil
}

func hasKey[S any](m map[string]S, k string) bool { _, ok := m[k]; return ok }

// badKeyJugs drops S from the key, merging states that behave differently.
type badKeyJugs struct{ models.Jugs }

func (badKeyJugs) AppendKey(buf []byte, j models.Jug) []byte { return append(buf, byte(j.B)) }

func TestKeyInjectivity(t *testing.T) {
	checks := map[string]error{
		"jugs":  keyInjective(models.Jugs{}, models.NotFour),
		"grid2": keyInjective(models.MultiInit, models.Sum),
		"gridN": keyInjective(models.GridN{D: 3, K: 4}, nil),
	}
	for name, err := range checks {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if keyInjective(badKeyJugs{}, models.NotFour) == nil {
		t.Error("a key that drops the small jug was accepted")
	}
}

// mutating emits each successor and then changes it, breaking the ownership
// rule: the queued state no longer matches its stored key.
type mutating struct{ models.GridN }

func (m mutating) Next(s []int, emit func(int, []int) bool) {
	m.GridN.Next(s, func(i int, t []int) bool {
		ok := emit(i, t)
		t[i] = m.K - 1
		return ok
	})
}

func TestDetectMutation(t *testing.T) {
	m := mutating{models.GridN{D: 2, K: 3}}
	on, err := Run(context.Background(), m, Config[[]int]{DetectMutation: true})
	if err != nil || on.Status != ModelError || !errors.Is(on.Err, ErrStateMutated) {
		t.Errorf("detector on: %v %v %v", on.Status, on.Err, err)
	}
	if off, _ := Run(context.Background(), m, Config[[]int]{}); off.Status == ModelError {
		t.Errorf("detector off: got %v, want the mutation to go unnoticed", off.Status)
	}
	if ok, _ := Run(context.Background(), models.GridN{D: 3, K: 4}, Config[[]int]{DetectMutation: true}); ok.Status != Exhausted {
		t.Errorf("well-behaved model with detector: %v %v", ok.Status, ok.Err)
	}
}
