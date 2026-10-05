package models_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/reference"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

var (
	noLost     = check.Invariant[models.QueueState]{Name: "NoLostTask", Holds: models.NoLostTask}
	atMostOnce = check.Invariant[models.QueueState]{Name: "AtMostOnce", Holds: models.AtMostOnce}
)

func queueHolds(s models.QueueState) bool { return models.NoLostTask(s) && models.AtMostOnce(s) }

func runQueue(t *testing.T, m models.TaskQueue) check.Result[models.QueueState, models.QAction] {
	t.Helper()
	res, err := check.Run(context.Background(), m, check.Config[models.QueueState]{
		Invariants: []check.Invariant[models.QueueState]{noLost, atMostOnce}, DetectMutation: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// The correct queue is Exhausted at every size. Terminal states have every
// task acked and every worker idle; they differ only in crashes left, so
// there are Crashes+1 of them. The 1×1×0 and 1×1×1 rows were derived by hand
// (docs/CONFORMANCE.md, "Task queue"); every row is also checked against the
// reference explorer.
func TestTaskQueueCorrect(t *testing.T) {
	for _, tc := range []struct {
		tasks, workers, crashes int
		states, transitions     int64
		depth                   int
	}{
		{1, 1, 0, 4, 3, 3},
		{1, 1, 1, 10, 10, 5},
		{2, 2, 1, 84, 184, 8},
		{2, 2, 2, 147, 368, 10},
		{3, 2, 2, 702, 2070, 13},
		{3, 3, 2, 1836, 7245, 13},
		{4, 3, 3, 15768, 75972, 18},
	} {
		m := models.TaskQueue{Tasks: tc.tasks, Workers: tc.workers, Crashes: tc.crashes}
		res := runQueue(t, m)
		s := res.Stats
		if res.Status != check.Exhausted || s.Admitted != tc.states || s.Transitions != tc.transitions ||
			s.TerminalStates != int64(tc.crashes+1) || s.MaxDepth != tc.depth {
			t.Errorf("%+v: %v", m, res)
		}
		ref := reference.Explore(m, queueHolds, -1, -1)
		if ref.Status != "Exhausted" || int64(ref.Admitted) != tc.states || int64(ref.Edges) != tc.transitions {
			t.Errorf("%+v: reference explorer: %s, %d states, %d edges", m, ref.Status, ref.Admitted, ref.Edges)
		}
	}
}

// TestRegression_TaskQueueBugs pins both broken variants' shortest
// counterexamples. AckOnDelivery: a crash right after delivery loses the
// task. NonIdempotent: a crash between run and ack makes the retry apply the
// effect a second time.
func TestRegression_TaskQueueBugs(t *testing.T) {
	lost := []string{
		"A takes t0; the queue acks it on delivery",
		"A crashes holding t0; already acked, so it is not requeued",
	}
	double := []string{
		"A takes t0",
		"A runs t0: effect applied (run 1)",
		"A crashes holding t0; lease expires, t0 requeued",
		"A takes t0",
		"A runs t0: effect applied (run 2)",
	}
	for _, tc := range []struct {
		m       models.TaskQueue
		inv     check.Invariant[models.QueueState]
		actions []string
		states  int64
	}{
		{models.TaskQueue{Tasks: 1, Workers: 1, Crashes: 1, Bug: models.AckOnDelivery}, noLost, lost, 4},
		{models.TaskQueue{Tasks: 2, Workers: 2, Crashes: 1, Bug: models.AckOnDelivery}, noLost, lost, 7},
		{models.TaskQueue{Tasks: 1, Workers: 1, Crashes: 1, Bug: models.NonIdempotent}, atMostOnce, double, 10},
		{models.TaskQueue{Tasks: 2, Workers: 2, Crashes: 1, Bug: models.NonIdempotent}, atMostOnce, double, 48},
		{models.TaskQueue{Tasks: 3, Workers: 3, Crashes: 2, Bug: models.NonIdempotent}, atMostOnce, double, 218},
	} {
		res := runQueue(t, tc.m)
		tracetest.Expect(t, tc.m, tc.inv, res, tc.actions...)
		ref := reference.Explore(tc.m, queueHolds, -1, -1)
		if res.Stats.Admitted != tc.states || ref.Status != "Violation" || int64(ref.Admitted) != tc.states || ref.ViolationDepth != len(tc.actions) {
			t.Errorf("%+v: %d states; reference %s, %d states, depth %d", tc.m, res.Stats.Admitted, ref.Status, ref.Admitted, ref.ViolationDepth)
		}
	}
}

// Without crashes neither bug can show: every delivered task is run and
// acked exactly once.
func TestTaskQueueBugsNeedACrash(t *testing.T) {
	for _, bug := range []models.QueueBug{models.AckOnDelivery, models.NonIdempotent} {
		if res := runQueue(t, models.TaskQueue{Tasks: 3, Workers: 2, Bug: bug}); res.Status != check.Exhausted {
			t.Errorf("bug %d without crashes: %v", bug, res)
		}
	}
}

func TestTaskQueueFormatting(t *testing.T) {
	s := models.QueueState{T: 3, W: 2, CrashesLeft: 1,
		Tasks:   [4]models.QTask{{Acked: true}, {Runs: 2}, {Acked: true, Runs: 1}},
		Workers: [3]models.QWorker{{Phase: models.QRan, Task: 1}}}
	const want = "t0 acked runs=0, t1 held runs=2, t2 acked runs=1 | A ran t1 | B idle | crashes left 1" +
		" | LOST: a task was acked but never run, and no worker holds it | DOUBLE: a task's effect was applied twice"
	if got := s.String(); got != want {
		t.Errorf("state:\n got %q\nwant %q", got, want)
	}
	for a, want := range map[models.QAction]string{
		{Kind: models.QRun, Worker: 1, Task: 2, Skipped: true, Runs: 1}: "B runs t2: store already has its result, skipped",
		{Kind: models.QAck, Worker: 2, Task: 3}:                         "C acks t3: done",
		{Kind: 9}:                                                       "QKind(9)",
	} {
		if got := a.String(); got != want {
			t.Errorf("action %+v = %q, want %q", a, got, want)
		}
	}
}

func TestTaskQueueRejectsBadSize(t *testing.T) {
	if res := runQueue(t, models.TaskQueue{Tasks: 5, Workers: 1}); res.Status != check.ModelError {
		t.Errorf("status = %v, want ModelError", res.Status)
	}
}
