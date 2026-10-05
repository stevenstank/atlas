package check

import (
	"errors"
	"fmt"
)

// DefaultCheckInterval is the default K in the interruption schedule
// (DECISIONS.md D-012): checks run before dequeues 1, 1+K, 1+2K, ...
const DefaultCheckInterval = 1024

// ErrInvalidConfig is wrapped by every configuration error returned by Run.
// It is a caller error, distinct from ModelError.
var ErrInvalidConfig = errors.New("check: invalid configuration")

// Limit is an optional limit. The zero value means "no limit", which is
// distinct from Max(0).
type Limit struct {
	n   int
	set bool
}

// Max returns a limit of n.
func Max(n int) Limit { return Limit{n: n, set: true} }

// Get returns the limit and whether it is set.
func (l Limit) Get() (n int, ok bool) { return l.n, l.set }

// Invariant is a named predicate that must hold in every admitted state.
type Invariant[S any] struct {
	Name  string
	Holds func(S) bool
}

// Config configures a model-checking run. Time limits are expressed through
// the context passed to Run.
type Config[S any] struct {
	// Invariants are checked in order on every admitted state.
	Invariants []Invariant[S]
	// MaxDepth is the maximum depth of states admitted and checked. States at
	// that depth are still expanded to detect cutoffs (D-012). Must be >= 0.
	MaxDepth Limit
	// MaxStates is the maximum number of admitted states, including initial
	// states. Must be >= 1.
	MaxStates Limit
	// MaxHeapBytes ends the run as Incomplete when the sampled live heap
	// exceeds it at a scheduled check. 0 means no limit.
	MaxHeapBytes uint64
	// CheckInterval is K in the interruption schedule. 0 means
	// DefaultCheckInterval. Tests that need deterministic interruption use 1.
	CheckInterval int
	// DetectMutation re-encodes each state when it is dequeued and compares
	// it with the key stored at admission (TESTING.md §3). A mismatch means an
	// emitted state was mutated, and the run ends with ErrStateMutated. It
	// costs one extra AppendKey per expansion; intended for tests.
	DetectMutation bool
}

func (c *Config[S]) validate() error {
	if n, ok := c.MaxDepth.Get(); ok && n < 0 {
		return fmt.Errorf("%w: MaxDepth is %d, must be >= 0", ErrInvalidConfig, n)
	}
	if n, ok := c.MaxStates.Get(); ok && n < 1 {
		return fmt.Errorf("%w: MaxStates is %d, must be >= 1", ErrInvalidConfig, n)
	}
	if c.CheckInterval < 0 {
		return fmt.Errorf("%w: CheckInterval is %d, must be >= 0", ErrInvalidConfig, c.CheckInterval)
	}
	for i, inv := range c.Invariants {
		if inv.Holds == nil {
			return fmt.Errorf("%w: invariant %d (%q) has nil Holds", ErrInvalidConfig, i, inv.Name)
		}
	}
	return nil
}
