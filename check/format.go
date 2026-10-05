package check

import (
	"fmt"
	"strings"
)

// Formatting hooks: a model needs no Atlas-specific interface to get readable
// output. States and actions are printed with fmt: "%+v" for states and "%v"
// for actions. A type that implements fmt.Stringer or fmt.Formatter controls
// its own text; any other type gets fmt's default rendering, with field names
// for structs.

// String renders the trace one step per entry: the action taken, then the
// resulting state. Step 0 is the initial state.
func (t Trace[S, A]) String() string {
	var b strings.Builder
	t.write(&b)
	return b.String()
}

func (t Trace[S, A]) write(b *strings.Builder) {
	for i, st := range t.Steps {
		if i == 0 {
			fmt.Fprintf(b, "  step 0: initial state\n")
		} else {
			fmt.Fprintf(b, "  step %d: %v\n", i, st.Action)
		}
		fmt.Fprintf(b, "          %+v\n", st.State)
	}
}

// String renders the failed invariant, the trace, and which state fails it.
func (c *Counterexample[S, A]) String() string {
	var b strings.Builder
	n := len(c.Trace.Steps) - 1
	fmt.Fprintf(&b, "counterexample: invariant %q fails after %d steps\n", c.Invariant, n)
	c.Trace.write(&b)
	fmt.Fprintf(&b, "  the state after step %d violates %q\n", n, c.Invariant)
	return b.String()
}

// String renders the verdict, the claim, the counterexample if there is one,
// and the main statistics. The first line starts with the status and says
// whether the run verified the model; only Exhausted does.
func (r Result[S, A]) String() string {
	var b strings.Builder
	switch r.Status {
	case Exhausted:
		fmt.Fprintf(&b, "Exhausted: verified: %s\n", r.Claim())
	case Violation:
		fmt.Fprintf(&b, "Violation: %s\n", r.Claim())
		if r.Violation != nil {
			b.WriteString(r.Violation.String())
		}
	default: // Bounded, Incomplete, ModelError, and any unknown status
		fmt.Fprintf(&b, "%s: NOT VERIFIED", r.Status)
		if r.Reason != NoReason {
			fmt.Fprintf(&b, " (%s)", r.Reason)
		}
		fmt.Fprintf(&b, ": %s\n", r.Claim())
	}
	s := r.Stats
	fmt.Fprintf(&b, "states %d, transitions %d, terminal states %d, max depth %d\n",
		s.Admitted, s.Transitions, s.TerminalStates, s.MaxDepth)
	return b.String()
}
