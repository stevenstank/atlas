// Package reference is a deliberately naive explorer used only to cross-check
// package check in tests (docs/TESTING.md §1). It shares no traversal code
// with core or check: it collects each Init/Next call's emissions into a
// slice, then applies the D-012 rules to them one by one, level by level.
package reference

import "github.com/stevenstank/atlas/core"

// Counts is what the reference run observed. Limits of -1 mean "none".
type Counts struct {
	Status                                      string // Exhausted, Bounded, Violation, ModelError
	StateLimited                                bool   // Bounded by the state limit (else by depth)
	InitEmissions, InitAdmitted, InitDuplicates int
	Admitted, Expanded, Transitions             int
	Duplicates, Cutoffs, Refusals               int
	RefusedInInit                               bool
	MaxDepth                                    int
	ViolationDepth                              int // depth of the violating state
	SuccessorSum                                int // Σ |Next(s)| over admitted states
	Edges                                       int // Σ |Next(s)| over all reachable states (unlimited run)
}

// Explore runs the naive BFS. ok is nil for "no invariant".
func Explore[S, A any](m core.Model[S, A], ok func(S) bool, maxDepth, maxStates int) Counts {
	c := Counts{MaxDepth: -1, ViolationDepth: -1}
	depth := map[string]int{}
	var admittedStates []S
	key := func(s S) string { return string(m.AppendKey(nil, s)) }
	succ := func(s S) []S {
		var out []S
		m.Next(s, func(_ A, t S) bool { out = append(out, t); return true })
		return out
	}
	// admit applies the per-emission rules; it returns false when the run ends.
	admit := func(t S, d int) bool {
		k := key(t)
		if _, seen := depth[k]; seen {
			return true
		}
		depth[k] = d
		admittedStates = append(admittedStates, t)
		c.Admitted++
		c.MaxDepth = max(c.MaxDepth, d)
		if ok != nil && !ok(t) {
			c.Status, c.ViolationDepth = "Violation", d
			return false
		}
		return true
	}
	full := func() bool { return maxStates >= 0 && c.Admitted >= maxStates }

	var inits []S
	m.Init(func(s S) bool { inits = append(inits, s); return true })
	var level []S
	for _, s := range inits {
		c.InitEmissions++
		if _, seen := depth[key(s)]; seen {
			c.InitDuplicates++
			continue
		}
		if full() {
			c.Refusals, c.RefusedInInit = 1, true
			c.Status, c.StateLimited = "Bounded", true
			return c
		}
		c.InitAdmitted++
		if !admit(s, 0) {
			return c
		}
		level = append(level, s)
	}
	if len(inits) == 0 {
		c.Status = "ModelError"
		return c
	}
	for d := 0; len(level) > 0; d++ {
		var next []S
		for _, s := range level {
			c.Expanded++
			for _, t := range succ(s) {
				c.Transitions++
				if _, seen := depth[key(t)]; seen {
					c.Duplicates++
					continue
				}
				if maxDepth >= 0 && d+1 > maxDepth {
					c.Cutoffs++
					continue
				}
				if full() {
					c.Refusals = 1
					c.Status, c.StateLimited = "Bounded", true
					return c
				}
				if !admit(t, d+1) {
					return c
				}
				next = append(next, t)
			}
		}
		level = next
	}
	for _, s := range admittedStates {
		c.SuccessorSum += len(succ(s))
	}
	c.Status = "Exhausted"
	if c.Cutoffs > 0 {
		c.Status = "Bounded"
	}
	if maxDepth < 0 && maxStates < 0 && c.Status == "Exhausted" {
		c.Edges = c.SuccessorSum
	}
	return c
}
