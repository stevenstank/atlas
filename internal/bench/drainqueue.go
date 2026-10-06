package bench

import (
	"fmt"

	"github.com/stevenstank/atlas/models"
)

// EarlyClose is the B7 workload: models.TaskQueue with a bug in when the
// queue closes. The queue is meant to close once every task is acked. This
// variant closes once every task has been delivered at least once. A closed
// queue accepts no requeues, so a worker that crashes after the close loses
// its task: the queue drops it instead of requeueing it.
//
// Every step is a models.TaskQueue step, unchanged, except a crash after the
// close. The state adds only the set of tasks delivered so far.
//
// Why the counterexample is deep: the bug needs the queue closed, so every
// task must have been delivered, and a worker must then crash before running
// its task. With W workers and T >= W tasks, a worker can hold at most W
// delivered tasks at once, so T-W tasks must be taken, run, and acked (3 steps
// each), W more must be taken, and one holder crashes: 3T - 2W + 1 steps. The
// shortest trace grows by 3 steps per task, and every step is real queue work.
type EarlyClose struct{ models.TaskQueue }

// ECState is an EarlyClose state. Bit t of Delivered is task t.
type ECState struct {
	Q         models.QueueState
	Delivered uint8
}

// ECAction is a TaskQueue step; Dropped marks a crash after the close.
type ECAction struct {
	models.QAction
	Dropped bool
}

func (a ECAction) String() string {
	if a.Dropped {
		return fmt.Sprintf("%s crashes holding t%d; the queue is closed (every task delivered), so t%d is dropped",
			string(rune('A'+a.Worker)), a.Task, a.Task)
	}
	return a.QAction.String()
}

func (e EarlyClose) Init(emit func(ECState) bool) {
	e.TaskQueue.Init(func(q models.QueueState) bool { return emit(ECState{Q: q}) })
}

func (e EarlyClose) Next(s ECState, emit func(ECAction, ECState) bool) {
	closed := s.Delivered == 1<<s.Q.T-1
	e.TaskQueue.Next(s.Q, func(a models.QAction, q models.QueueState) bool {
		t := ECState{Q: q, Delivered: s.Delivered}
		if a.Kind == models.QTake {
			t.Delivered |= 1 << a.Task
		}
		drop := closed && a.Kind == models.QCrash
		if drop {
			t.Q.Tasks[a.Task].Acked = true // removed from the queue for good
		}
		return emit(ECAction{QAction: a, Dropped: drop}, t)
	})
}

func (e EarlyClose) AppendKey(buf []byte, s ECState) []byte {
	return append(e.TaskQueue.AppendKey(buf, s.Q), s.Delivered)
}

// ECNoLostTask is models.NoLostTask on the queue state.
func ECNoLostTask(s ECState) bool { return models.NoLostTask(s.Q) }
