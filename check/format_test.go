package check_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/models"
)

// Jug and string have no custom formatting, so this is the fallback: fmt's
// "%+v" for states and "%v" for actions. Counts are CONFORMANCE.md row J2.
func TestCounterexampleFallbackFormat(t *testing.T) {
	res := run(t, context.Background(), models.Jugs{}, jugCfg{Invariants: notFour})
	const want = `Violation: invariant "NotFour" is violated; counterexample has 6 steps
counterexample: invariant "NotFour" fails after 6 steps
  step 0: initial state
          {B:0 S:0}
  step 1: FillBig
          {B:5 S:0}
  step 2: BigToSmall
          {B:2 S:3}
  step 3: EmptySmall
          {B:2 S:0}
  step 4: BigToSmall
          {B:0 S:2}
  step 5: FillBig
          {B:5 S:2}
  step 6: BigToSmall
          {B:4 S:3}
  the state after step 6 violates "NotFour"
states 13, transitions 38, terminal states 0, max depth 6
`
	if got := res.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Only Exhausted may present itself as verified. Every other status must say
// NOT VERIFIED (or, for Violation, show the counterexample) on its first line,
// so that printing a Bounded or Incomplete result cannot read as success.
func TestOnlyExhaustedReadsAsVerified(t *testing.T) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()
	for _, tc := range []struct {
		res    check.Result[models.Jug, string]
		status check.Status
		first  string
	}{
		{run(t, bg, models.Jugs{}, jugCfg{}), check.Exhausted,
			"Exhausted: verified: no reachable state violates any invariant"},
		{run(t, bg, models.Jugs{}, jugCfg{MaxDepth: check.Max(5)}), check.Bounded,
			"Bounded: NOT VERIFIED (DepthLimit): no invariant violation in any state at depth <= 5; deeper states were not checked"},
		{run(t, bg, models.Jugs{}, jugCfg{MaxStates: check.Max(15)}), check.Bounded,
			"Bounded: NOT VERIFIED (StateLimit): no invariant violation among the 15 admitted states"},
		{run(t, canceled, models.Jugs{}, jugCfg{}), check.Incomplete,
			"Incomplete: NOT VERIFIED (Canceled): no conclusion: run interrupted (Canceled)"},
		{run(t, bg, models.Jugs{}, jugCfg{Invariants: notFour}), check.Violation,
			`Violation: invariant "NotFour" is violated`},
	} {
		got := tc.res.String()
		first, _, _ := strings.Cut(got, "\n")
		if tc.res.Status != tc.status || !strings.HasPrefix(first, tc.first) {
			t.Errorf("%v: first line %q, want prefix %q", tc.status, first, tc.first)
		}
		if verified := strings.Contains(strings.ToLower(got), "verified") && !strings.Contains(got, "NOT VERIFIED"); verified != (tc.status == check.Exhausted) {
			t.Errorf("%v reads as verified = %v:\n%s", tc.status, verified, got)
		}
	}
	me := run(t, bg, models.Grid2{Inits: []models.Cell{}}, cellCfg{})
	if got := me.String(); !strings.HasPrefix(got, "ModelError: NOT VERIFIED: no conclusion: check: model emitted no initial states\n") {
		t.Errorf("ModelError: %q", got)
	}
}

// Statuses outside the enum (the zero value included) are not success.
func TestUnknownStatusIsNotVerified(t *testing.T) {
	for _, s := range []check.Status{0, 99} {
		got := check.Result[models.Jug, string]{Status: s}.String()
		if !strings.Contains(got, "NOT VERIFIED") {
			t.Errorf("%v: %q", s, got)
		}
	}
}
