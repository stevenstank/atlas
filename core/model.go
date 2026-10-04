// Package core holds the pieces shared by Atlas applications: the model
// contract, exact state storage, and the frontier queue.
package core

// Model is a transition system to explore (DECISIONS.md D-001, option 3b).
//
// Contract:
//   - Init and Next are pure, and emit in a deterministic order.
//   - emit returns true to continue. false means stop: make no further emit
//     calls and return promptly. false never means "skip this one".
//   - emit may be called only synchronously, from the goroutine running Init
//     or Next, during that call.
//   - Emitted states belong to the engine and must not be mutated afterwards.
//   - AppendKey appends the canonical encoding of s to buf. Two states are
//     the same state if and only if their keys are equal (D-002).
type Model[S, A any] interface {
	Init(emit func(S) bool)
	Next(s S, emit func(A, S) bool)
	AppendKey(buf []byte, s S) []byte
}
