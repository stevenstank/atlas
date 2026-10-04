package check

import (
	"errors"
	"fmt"
	"time"
)

// Status is the outcome of a run (DECISIONS.md D-012). Only Exhausted is a
// verification result.
type Status int

const (
	// Exhausted: every reachable state was admitted and satisfies every invariant.
	Exhausted Status = iota + 1
	// Bounded: a configured limit excluded at least one reachable state, and
	// no admitted state violates an invariant.
	Bounded
	// Incomplete: the run was interrupted before reaching a conclusion.
	Incomplete
	// Violation: an admitted state violates an invariant. See Result.Violation.
	Violation
	// ModelError: the model panicked or broke its contract. See Result.Err.
	ModelError
)

func (s Status) String() string {
	switch s {
	case Exhausted:
		return "Exhausted"
	case Bounded:
		return "Bounded"
	case Incomplete:
		return "Incomplete"
	case Violation:
		return "Violation"
	case ModelError:
		return "ModelError"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// StopReason says which limit or interruption produced Bounded or Incomplete.
type StopReason int

const (
	NoReason StopReason = iota
	DepthLimit
	StateLimit
	Canceled
	DeadlineExceeded
	MemoryLimit
	CapacityLimit // more states than a core.Store can index
)

func (r StopReason) String() string {
	switch r {
	case NoReason:
		return "none"
	case DepthLimit:
		return "DepthLimit"
	case StateLimit:
		return "StateLimit"
	case Canceled:
		return "Canceled"
	case DeadlineExceeded:
		return "DeadlineExceeded"
	case MemoryLimit:
		return "MemoryLimit"
	case CapacityLimit:
		return "CapacityLimit"
	}
	return fmt.Sprintf("StopReason(%d)", int(r))
}

// Phase is where a state-limit refusal happened.
type Phase int

const (
	NoPhase Phase = iota
	InitPhase
	ExpansionPhase
)

// Stats are counts of what the engine examined (DECISIONS.md D-012,
// "Statistics and accounting identities"). An emission is counted only when
// it is assigned to exactly one branch.
type Stats struct {
	InitEmissions      int64
	InitAdmitted       int64
	InitDuplicates     int64
	Admitted           int64 // all admitted states (StatesDiscovered)
	Expanded           int64 // states whose Next was called
	Transitions        int64 // successor emissions examined
	Duplicates         int64
	CutoffTransitions  int64
	StateLimitRefusals int64 // 0 or 1
	RefusalPhase       Phase
	TerminalStates     int64 // expanded states whose Next emitted nothing
	MaxDepth           int   // -1 when no state was admitted
	FrontierPeak       int
	WallTime           time.Duration
}

// Step is one step of a trace. The first step's Action is the zero value.
type Step[S, A any] struct {
	Action A
	State  S
}

// Trace is a path from an initial state to a violating state.
type Trace[S, A any] struct {
	Steps []Step[S, A]
}

// Counterexample describes a violation.
type Counterexample[S, A any] struct {
	Invariant string
	Trace     Trace[S, A]
}

// Errors carried by Result.Err when Status is ModelError.
var (
	ErrNoInitialStates  = errors.New("check: model emitted no initial states")
	ErrCallbackContract = errors.New("check: model violated the emit contract")
	ErrNondeterministic = errors.New("check: model is nondeterministic on replay")
)

// PanicError is a panic recovered from model code.
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string { return fmt.Sprintf("check: model panicked: %v", e.Value) }

// Result is the outcome of Run.
type Result[S, A any] struct {
	Status    Status
	Reason    StopReason
	Violation *Counterexample[S, A] // set when Status == Violation
	Err       error                 // set when Status == ModelError
	Stats     Stats

	limit         int // the depth or state limit behind Bounded
	completeBelow int // for StateLimit: every state shallower than this was admitted
}

// Claim states exactly what the result shows.
func (r Result[S, A]) Claim() string {
	switch r.Status {
	case Exhausted:
		return "no reachable state violates any invariant"
	case Bounded:
		if r.Reason == DepthLimit {
			return fmt.Sprintf("no invariant violation in any state at depth <= %d; deeper states were not checked", r.limit)
		}
		if r.completeBelow == 0 {
			return fmt.Sprintf("no invariant violation among the %d admitted initial states; the initial set itself was not fully examined", r.limit)
		}
		return fmt.Sprintf("no invariant violation among the %d admitted states, which include every state at depth < %d; other reachable states were not checked", r.limit, r.completeBelow)
	case Incomplete:
		return fmt.Sprintf("no conclusion: run interrupted (%s)", r.Reason)
	case Violation:
		return fmt.Sprintf("invariant %q is violated; counterexample has %d steps",
			r.Violation.Invariant, len(r.Violation.Trace.Steps)-1)
	case ModelError:
		return fmt.Sprintf("no conclusion: %v", r.Err)
	}
	return "no result"
}
