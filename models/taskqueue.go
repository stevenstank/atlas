package models

import (
	"fmt"
	"strings"

	"github.com/stevenstank/atlas/core"
)

// TaskQueue is an at-least-once task queue with leases, retries, and worker
// crashes (ROADMAP Phase 2).
//
// State. Each task is queued, held by a worker, or acked (the queue has
// removed it for good). Runs counts how many times the task's effect was
// applied to the result store, saturating at 2. Each worker is idle, holds a
// task it has not run yet, or holds a task it has run but not acked.
// CrashesLeft bounds the crashes in the whole run.
//
// Actions, per worker in order A, B, C, in this order when enabled:
//   - an idle worker takes any queued task, lowest index first (queue order is
//     not modeled);
//   - a worker holding an unrun task runs it: the result store applies the
//     effect unless it already recorded one for that task (an idempotency
//     key), in which case the run is skipped;
//   - a worker holding a run task acks it, and the task is done;
//   - a worker holding a task crashes (if crashes remain). Its lease expires
//     and the task goes back on the queue for a retry; the worker restarts
//     idle. Lease expiry is folded into the crash: a live worker's lease never
//     expires, so slow-but-alive workers are not modeled.
//
// Broken variants (Bug):
//   - AckOnDelivery: the queue acks a task when it is delivered, before it
//     runs (at-most-once). A crash then loses the task.
//   - NonIdempotent: the result store applies every run, so a retry after a
//     crash between run and ack applies the effect twice.
//
// Invariants. NoLostTask: every task whose effect has not been applied is
// queued or held by a worker. AtMostOnce: no task's effect is applied twice.
// That every task eventually completes is a liveness property, out of scope;
// the terminal states (all acked, all workers idle) show it can.
//
// Finiteness. Every field is bounded: Runs saturates at 2, CrashesLeft only
// decreases, and the other fields take a few values each. Without crashes
// every task is taken, run, and acked once; each crash adds at most one retry.
type TaskQueue struct {
	Tasks, Workers, Crashes int // 1-4 tasks, 1-3 workers, 0-9 crashes
	Bug                     QueueBug
}

// QueueBug selects a broken TaskQueue variant.
type QueueBug uint8

const (
	NoBug QueueBug = iota
	AckOnDelivery
	NonIdempotent
)

// QWorkerPhase is what a worker is doing.
type QWorkerPhase uint8

const (
	QIdle    QWorkerPhase = iota
	QHolding              // holds Task, not run yet
	QRan                  // holds Task, run, not acked
)

// QWorker is one worker.
type QWorker struct {
	Phase QWorkerPhase
	Task  uint8 // 0 when idle
}

// QTask is one task's queue and result-store state.
type QTask struct {
	Acked bool
	Runs  uint8
}

// QueueState is a TaskQueue state. Unused slots stay zero.
type QueueState struct {
	T, W        uint8
	Tasks       [4]QTask
	Workers     [3]QWorker
	CrashesLeft uint8
}

// QKind is the kind of a QAction.
type QKind uint8

const (
	QTake QKind = iota
	QRun
	QAck
	QCrash
)

// QAction is one worker step. Acked on QTake means the queue acked on
// delivery; on QCrash it means the task was already acked and is not
// requeued. Runs on QRun is the task's run count afterwards, Skipped that the
// store ignored the run.
type QAction struct {
	Kind           QKind
	Worker, Task   uint8
	Acked, Skipped bool
	Runs           uint8
}

func (q TaskQueue) Init(emit func(QueueState) bool) {
	if q.Tasks < 1 || q.Tasks > 4 || q.Workers < 1 || q.Workers > 3 || q.Crashes < 0 || q.Crashes > 9 {
		panic(fmt.Sprintf("models: TaskQueue needs 1-4 tasks, 1-3 workers, 0-9 crashes, got %d, %d, %d", q.Tasks, q.Workers, q.Crashes))
	}
	emit(QueueState{T: uint8(q.Tasks), W: uint8(q.Workers), CrashesLeft: uint8(q.Crashes)})
}

// held reports whether some worker holds task t.
func (s QueueState) held(t uint8) bool {
	for _, w := range s.Workers[:s.W] {
		if w.Phase != QIdle && w.Task == t {
			return true
		}
	}
	return false
}

func (q TaskQueue) Next(s QueueState, emit func(QAction, QueueState) bool) {
	for i := range s.W {
		w := s.Workers[i]
		switch w.Phase {
		case QIdle:
			for t := range s.T {
				if s.Tasks[t].Acked || s.held(t) {
					continue
				}
				n := s
				n.Workers[i] = QWorker{Phase: QHolding, Task: t}
				n.Tasks[t].Acked = q.Bug == AckOnDelivery
				if !emit(QAction{Kind: QTake, Worker: i, Task: t, Acked: n.Tasks[t].Acked}, n) {
					return
				}
			}
			continue
		case QHolding:
			n := s
			tk := &n.Tasks[w.Task]
			skip := tk.Runs > 0 && q.Bug != NonIdempotent
			if !skip {
				tk.Runs = min(tk.Runs+1, 2)
			}
			n.Workers[i].Phase = QRan
			if !emit(QAction{Kind: QRun, Worker: i, Task: w.Task, Skipped: skip, Runs: tk.Runs}, n) {
				return
			}
		case QRan:
			n := s
			n.Workers[i] = QWorker{}
			n.Tasks[w.Task].Acked = true
			if !emit(QAction{Kind: QAck, Worker: i, Task: w.Task}, n) {
				return
			}
		}
		if s.CrashesLeft > 0 {
			n := s
			n.Workers[i] = QWorker{}
			n.CrashesLeft--
			if !emit(QAction{Kind: QCrash, Worker: i, Task: w.Task, Acked: s.Tasks[w.Task].Acked}, n) {
				return
			}
		}
	}
}

func (TaskQueue) AppendKey(buf []byte, s QueueState) []byte {
	buf = append(buf, s.T, s.W, s.CrashesLeft)
	for _, t := range s.Tasks[:s.T] {
		buf = append(buf, t.Runs)
		buf = core.AppendBool(buf, t.Acked)
	}
	for _, w := range s.Workers[:s.W] {
		buf = append(buf, byte(w.Phase), w.Task)
	}
	return buf
}

// NoLostTask is a TaskQueue invariant: a task whose effect has not been
// applied is still queued or held by a worker.
func NoLostTask(s QueueState) bool {
	for t := range s.T {
		if s.Tasks[t].Runs == 0 && s.Tasks[t].Acked && !s.held(t) {
			return false
		}
	}
	return true
}

// AtMostOnce is a TaskQueue invariant: no task's effect is applied twice.
func AtMostOnce(s QueueState) bool {
	for _, t := range s.Tasks[:s.T] {
		if t.Runs > 1 {
			return false
		}
	}
	return true
}

func (a QAction) String() string {
	w, t := clientName(a.Worker), fmt.Sprintf("t%d", a.Task)
	switch a.Kind {
	case QTake:
		if a.Acked {
			return fmt.Sprintf("%s takes %s; the queue acks it on delivery", w, t)
		}
		return fmt.Sprintf("%s takes %s", w, t)
	case QRun:
		if a.Skipped {
			return fmt.Sprintf("%s runs %s: store already has its result, skipped", w, t)
		}
		return fmt.Sprintf("%s runs %s: effect applied (run %d)", w, t, a.Runs)
	case QAck:
		return fmt.Sprintf("%s acks %s: done", w, t)
	case QCrash:
		if a.Acked {
			return fmt.Sprintf("%s crashes holding %s; already acked, so it is not requeued", w, t)
		}
		return fmt.Sprintf("%s crashes holding %s; lease expires, %s requeued", w, t, t)
	}
	return fmt.Sprintf("QKind(%d)", a.Kind)
}

func (s QueueState) String() string {
	var b strings.Builder
	for t := range s.T {
		task := s.Tasks[t]
		if t > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "t%d ", t)
		switch {
		case s.held(t) && task.Acked:
			b.WriteString("held but already acked")
		case s.held(t):
			b.WriteString("held")
		case task.Acked:
			b.WriteString("acked")
		default:
			b.WriteString("queued")
		}
		fmt.Fprintf(&b, " runs=%d", task.Runs)
	}
	for i, w := range s.Workers[:s.W] {
		fmt.Fprintf(&b, " | %s ", clientName(uint8(i)))
		switch w.Phase {
		case QIdle:
			b.WriteString("idle")
		case QHolding:
			fmt.Fprintf(&b, "holds t%d", w.Task)
		case QRan:
			fmt.Fprintf(&b, "ran t%d", w.Task)
		}
	}
	fmt.Fprintf(&b, " | crashes left %d", s.CrashesLeft)
	if !NoLostTask(s) {
		b.WriteString(" | LOST: a task was acked but never run, and no worker holds it")
	}
	if !AtMostOnce(s) {
		b.WriteString(" | DOUBLE: a task's effect was applied twice")
	}
	return b.String()
}
