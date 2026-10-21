package optimize

import "fmt"

// Status is the outcome of a search (OPTIMIZATION.md §3). Only Optimal and
// Infeasible are proofs, and only over the declared domains.
type Status int

const (
	// Optimal: every assignment was evaluated; Assignment is a best feasible
	// one, the first enumerated among equals.
	Optimal Status = iota + 1
	// Infeasible: every assignment was evaluated and none is feasible.
	Infeasible
	// BestFound: the search stopped early after finding a feasible
	// assignment. Assignment is the best found so far, not proven optimal.
	BestFound
	// NoneFound: the search stopped early before finding a feasible
	// assignment. Feasibility is unknown.
	NoneFound
	// ModelError: a callback panicked. See Result.Err.
	ModelError
)

func (s Status) String() string {
	switch s {
	case Optimal:
		return "Optimal"
	case Infeasible:
		return "Infeasible"
	case BestFound:
		return "BestFound"
	case NoneFound:
		return "NoneFound"
	case ModelError:
		return "ModelError"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// StopReason says why a search ended as BestFound or NoneFound.
type StopReason int

const (
	NoReason StopReason = iota
	EvaluationLimit
	Canceled
	DeadlineExceeded
)

func (r StopReason) String() string {
	switch r {
	case NoReason:
		return "none"
	case EvaluationLimit:
		return "EvaluationLimit"
	case Canceled:
		return "Canceled"
	case DeadlineExceeded:
		return "DeadlineExceeded"
	}
	return fmt.Sprintf("StopReason(%d)", int(r))
}

// Stats count what the search examined.
type Stats struct {
	Evaluations int64 // assignments whose feasibility was decided
	Feasible    int64 // of those, the feasible ones (each had Objective called)
}

// PanicError is a panic recovered from a callback.
type PanicError struct {
	Value any
}

func (e *PanicError) Error() string { return fmt.Sprintf("optimize: callback panicked: %v", e.Value) }

// Result is the outcome of Solve.
type Result[V any] struct {
	Status     Status
	Reason     StopReason // set when Status is BestFound or NoneFound
	Assignment []V        // set when Status is Optimal or BestFound
	Value      int64      // the objective of Assignment; meaningful only when it is set
	Err        error      // set when Status is ModelError
	Stats      Stats
}
