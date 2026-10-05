package models_test

import (
	"testing"

	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/models"
)

// keyStates explores m with states identified by Go ==, not by AppendKey,
// and returns how many distinct states and distinct keys it reached. For a
// canonical, injective key (D-002) the two numbers are equal.
func keyStates[S comparable, A any](m core.Model[S, A]) (states, keys int) {
	seen := map[S]bool{}
	keySet := map[string]bool{}
	var queue []S
	add := func(s S) bool {
		if !seen[s] {
			seen[s] = true
			keySet[string(m.AppendKey(nil, s))] = true
			queue = append(queue, s)
		}
		return true
	}
	m.Init(add)
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		m.Next(s, func(_ A, t S) bool { return add(t) })
	}
	return len(seen), len(keySet)
}

func TestProtocolKeysInjective(t *testing.T) {
	for name, f := range map[string]func() (int, int){
		"register 3x2":       func() (int, int) { return keyStates(models.Register{Clients: 3, Ops: 2}) },
		"register 3x2 buggy": func() (int, int) { return keyStates(models.Register{Clients: 3, Ops: 2, NoInvalidate: true}) },
		"abp 4x3":            func() (int, int) { return keyStates(models.ABP{Msgs: 4, Cap: 3}) },
		"abp 3x2 buggy":      func() (int, int) { return keyStates(models.ABP{Msgs: 3, Cap: 2, IgnoreAckBit: true}) },
		"queue 4x3x3":        func() (int, int) { return keyStates(models.TaskQueue{Tasks: 4, Workers: 3, Crashes: 3}) },
		"queue 3x3x2 buggy": func() (int, int) {
			return keyStates(models.TaskQueue{Tasks: 3, Workers: 3, Crashes: 2, Bug: models.NonIdempotent})
		},
	} {
		if states, keys := f(); states != keys {
			t.Errorf("%s: %d distinct states but %d distinct keys", name, states, keys)
		}
	}
}
