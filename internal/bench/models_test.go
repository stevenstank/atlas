package bench_test

import (
	"context"
	"testing"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
	"github.com/stevenstank/atlas/internal/bench"
	"github.com/stevenstank/atlas/internal/reference"
	"github.com/stevenstank/atlas/internal/tracetest"
	"github.com/stevenstank/atlas/models"
)

func mustRun[S, A any](t *testing.T, m core.Model[S, A], cfg check.Config[S]) check.Stats {
	t.Helper()
	res, err := check.Run(context.Background(), m, cfg)
	if err != nil || res.Status != check.Exhausted {
		t.Fatalf("Run: %v, %v", err, res)
	}
	return res.Stats
}

// The 8-puzzle oracle: half of the 9! boards are reachable (TESTING.md §1).
// Within the reachable half each of the 9 blank positions occurs in 20,160
// boards, and the blank has 2 moves in a corner, 3 on an edge, and 4 in the
// centre, so there are 20,160 * (4*2 + 4*3 + 4) = 483,840 transitions. The
// reference explorer must agree, and gives the depth (31 moves).
func TestPuzzle8Counts(t *testing.T) {
	fact9 := int64(9 * 8 * 7 * 6 * 5 * 4 * 3 * 2)
	states, transitions := fact9/2, fact9/2/9*(4*2+4*3+4)
	s := mustRun(t, bench.Puzzle8{}, check.Config[bench.Board]{DetectMutation: true})
	ref := reference.Explore(bench.Puzzle8{}, nil, -1, -1)
	if s.Admitted != states || s.Transitions != transitions || s.MaxDepth != 31 || states != 181440 ||
		int64(ref.Admitted) != states || int64(ref.Edges) != transitions || ref.MaxDepth != 31 {
		t.Errorf("engine %d/%d depth %d; reference %d/%d depth %d; want %d/%d depth 31",
			s.Admitted, s.Transitions, s.MaxDepth, ref.Admitted, ref.Edges, ref.MaxDepth, states, transitions)
	}
}

// TwoPhase with one RM, by hand. Write (rm, tm, tmPrepared, msgs):
//
//	(w,i,-,{})       -> (p,i,-,{P}) (a,i,-,{}) (w,a,-,{A})
//	(p,i,-,{P})      -> (p,i,r,{P}) (p,a,-,{P,A})
//	(a,i,-,{})       -> (a,a,-,{A})
//	(w,a,-,{A})      -> (p,a,-,{P,A}) (a,a,-,{A}) (a,a,-,{A})  [prepare, choose abort, receive abort]
//	(p,i,r,{P})      -> (p,c,r,{P,C}) (p,a,r,{P,A})
//	(p,a,-,{P,A})    -> (a,a,-,{P,A})
//	(p,c,r,{P,C})    -> (c,c,r,{P,C})
//	(p,a,r,{P,A})    -> (a,a,r,{P,A})
//
// 12 states, 14 transitions, 4 terminal states (every aborted or committed
// end), depth 4. The larger sizes are checked against the reference
// explorer, which BENCHMARKS.md §3 names as the oracle for B5.
func TestTwoPhaseCounts(t *testing.T) {
	inv := check.Config[bench.TPState]{Invariants: tcConsistent, DetectMutation: true}
	if s := mustRun(t, bench.TwoPhase{N: 1}, inv); s.Admitted != 12 || s.Transitions != 14 || s.TerminalStates != 4 || s.MaxDepth != 4 {
		t.Errorf("N=1: %+v", s)
	}
	for _, c := range b5Counts {
		m := bench.TwoPhase{N: c.n}
		s := mustRun(t, m, inv)
		ref := reference.Explore(m, bench.TCConsistent, -1, -1)
		if s.Admitted != c.states || s.Transitions != c.transitions ||
			ref.Status != "Exhausted" || int64(ref.Admitted) != c.states || int64(ref.Edges) != c.transitions {
			t.Errorf("N=%d: engine %d/%d, reference %s %d/%d, recorded %d/%d",
				c.n, s.Admitted, s.Transitions, ref.Status, ref.Admitted, ref.Edges, c.states, c.transitions)
		}
	}
}

// A two-phase commit that lets the TM commit before every RM has prepared
// breaks TCConsistent, so the invariant is not vacuous.
type eagerCommit struct{ bench.TwoPhase }

func (e eagerCommit) Next(s bench.TPState, emit func(string, bench.TPState) bool) {
	if s.TM == bench.TMInit && s.TMPrepared != 0 && !s.Commit {
		t := s
		t.TM, t.Commit = bench.Committed, true
		if !emit("TMCommit(early)", t) {
			return
		}
	}
	e.TwoPhase.Next(s, emit)
}

func TestTCConsistentCanFail(t *testing.T) {
	res, _ := check.Run(context.Background(), eagerCommit{bench.TwoPhase{N: 2}}, check.Config[bench.TPState]{Invariants: tcConsistent})
	if res.Status != check.Violation || res.Violation.Invariant != "TCConsistent" {
		t.Errorf("got %v", res)
	}
}

// The payload changes no count: PaddedGrid matches GridN's analytic counts
// and the reference explorer for every B8 payload size.
func TestPaddedGridMatchesGridN(t *testing.T) {
	grid := mustRun(t, models.GridN{D: 3, K: 4}, check.Config[[]int]{})
	for _, pad := range []int{0, 64, 256} {
		g := bench.PaddedGrid{D: 3, K: 4, Pad: pad}
		s := mustRun(t, g, check.Config[[]byte]{DetectMutation: true})
		ref := reference.Explore(g, nil, -1, -1)
		if s.Admitted != 64 || s.Transitions != 3*3*16 || s.Admitted != grid.Admitted || s.Transitions != grid.Transitions ||
			s.MaxDepth != grid.MaxDepth || int64(ref.Admitted) != s.Admitted || int64(ref.Edges) != s.Transitions {
			t.Errorf("pad %d: %+v vs GridN %+v, reference %d/%d", pad, s, grid, ref.Admitted, ref.Edges)
		}
		var key []byte
		g.Init(func(b []byte) bool { key = g.AppendKey(nil, b); return true })
		if len(key) != 3+pad {
			t.Errorf("pad %d: key is %d bytes, want %d", pad, len(key), 3+pad)
		}
	}
}

func TestABPBenchCounts(t *testing.T) {
	holds := func(s models.ABPState) bool { return models.InOrderDelivery(s) && models.AckedDelivered(s) }
	for _, c := range b6Counts {
		m := models.ABP{Msgs: 3, Cap: c.cap}
		s := mustRun(t, m, check.Config[models.ABPState]{Invariants: abpInvariants, DetectMutation: true})
		ref := reference.Explore(m, holds, -1, -1)
		if s.Admitted != c.states || s.Transitions != c.transitions ||
			ref.Status != "Exhausted" || int64(ref.Admitted) != c.states || int64(ref.Edges) != c.transitions {
			t.Errorf("cap %d: engine %d/%d, reference %s %d/%d, recorded %d/%d",
				c.cap, s.Admitted, s.Transitions, ref.Status, ref.Admitted, ref.Edges, c.states, c.transitions)
		}
	}
}

var ecNoLost = check.Invariant[bench.ECState]{Name: "NoLostTask", Holds: bench.ECNoLostTask}

// EarlyClose's shortest counterexample has T + 2(T-W) - min(C-1, T-W) + 1
// steps for T tasks, W workers, and C crashes (T >= W): T takes, T-W releases
// at 2 steps (run, ack) or 1 (a spare crash), and the final crash. Both
// explorers must agree with the formula at every size the TaskQueue allows
// that has W <= T.
func TestEarlyCloseDepth(t *testing.T) {
	for tasks := 1; tasks <= 4; tasks++ {
		for w := 1; w <= min(3, tasks); w++ {
			for c := 1; c <= 3; c++ {
				m := bench.EarlyClose{TaskQueue: models.TaskQueue{Tasks: tasks, Workers: w, Crashes: c}}
				want := tasks + 2*(tasks-w) - min(c-1, tasks-w) + 1
				res, _ := check.Run(context.Background(), m, check.Config[bench.ECState]{Invariants: []check.Invariant[bench.ECState]{ecNoLost}})
				ref := reference.Explore(m, bench.ECNoLostTask, -1, -1)
				if res.Status != check.Violation || len(res.Violation.Trace.Steps)-1 != want || ref.ViolationDepth != want ||
					int64(ref.Admitted) != res.Stats.Admitted {
					t.Errorf("%dx%dx%d: %v; reference depth %d, %d states; want %d steps", tasks, w, c, res.Status, ref.ViolationDepth, ref.Admitted, want)
				}
			}
		}
	}
}

// The 4-task, 1-worker, 1-crash trace: three tasks go through the whole
// queue, the last is delivered (closing the queue), and its holder crashes.
func TestEarlyCloseReplay(t *testing.T) {
	m := bench.EarlyClose{TaskQueue: models.TaskQueue{Tasks: 4, Workers: 1, Crashes: 1}}
	res, _ := check.Run(context.Background(), m, check.Config[bench.ECState]{Invariants: []check.Invariant[bench.ECState]{ecNoLost}})
	tracetest.Expect(t, m, ecNoLost, res,
		"A takes t0", "A runs t0: effect applied (run 1)", "A acks t0: done",
		"A takes t1", "A runs t1: effect applied (run 1)", "A acks t1: done",
		"A takes t2", "A runs t2: effect applied (run 1)", "A acks t2: done",
		"A takes t3", "A crashes holding t3; the queue is closed (every task delivered), so t3 is dropped")
	// Without the bug the same workload loses nothing.
	ok, _ := check.Run(context.Background(), m.TaskQueue, check.Config[models.QueueState]{Invariants: []check.Invariant[models.QueueState]{{Name: "NoLostTask", Holds: models.NoLostTask}}})
	if ok.Status != check.Exhausted {
		t.Errorf("TaskQueue 4x1x1: %v", ok.Status)
	}
}
