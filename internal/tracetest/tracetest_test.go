package tracetest_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

// recorder is a testing.TB whose Fatalf records the failure and unwinds
// Expect with a panic, standing in for the real one's runtime.Goexit.
type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Fatalf(string, ...any) { r.failed = true; panic(r) }

func fails(f func(tb testing.TB)) bool {
	r := &recorder{}
	func() {
		defer func() {
			if p := recover(); p != nil && p != r {
				panic(p)
			}
		}()
		f(r)
	}()
	return r.failed
}

// Expect must fail when the bug is gone, when another invariant fails, or
// when the trace changes; and pass on the real counterexample.
func TestExpectRejectsChangedOutcomes(t *testing.T) {
	inv := check.Invariant[models.Jug]{Name: "NotFour", Holds: models.NotFour}
	other := check.Invariant[models.Jug]{Name: "Other", Holds: models.NotFour}
	trace := []string{"FillBig", "BigToSmall", "EmptySmall", "BigToSmall", "FillBig", "BigToSmall"}
	bug, _ := check.Run(context.Background(), models.Jugs{}, check.Config[models.Jug]{Invariants: []check.Invariant[models.Jug]{inv}})
	fixed, _ := check.Run(context.Background(), models.Jugs{}, check.Config[models.Jug]{})
	for name, tc := range map[string]struct {
		res     check.Result[models.Jug, string]
		inv     check.Invariant[models.Jug]
		actions []string
		fail    bool
	}{
		"matches":         {bug, inv, trace, false},
		"bug disappeared": {fixed, inv, trace, true},
		"wrong invariant": {bug, other, trace, true},
		"shorter trace":   {bug, inv, trace[:5], true},
		"other action":    {bug, inv, append(trace[:5:5], "FillSmall"), true},
	} {
		if got := fails(func(tb testing.TB) { tracetest.Expect(tb, models.Jugs{}, tc.inv, tc.res, tc.actions...) }); got != tc.fail {
			t.Errorf("%s: failed = %v, want %v", name, got, tc.fail)
		}
	}
}
