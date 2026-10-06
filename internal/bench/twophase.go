package bench

import "fmt"

// TwoPhase is two-phase commit for B5, following Lamport's TwoPhase TLA+
// specification (the standard TLA+ example; BENCHMARKS.md §9 names TLA+
// ports of B5 as a comparison candidate). N resource managers (RMs) and one
// transaction manager (TM) communicate through a message set that only
// grows: messages are never lost, reordered, or removed.
//
// State: each RM is working, prepared, committed, or aborted; the TM is
// init, committed, or aborted; TMPrepared is the set of RMs the TM has seen
// prepare; Msgs holds Prepared(r) messages and at most one Commit or Abort.
//
// Actions, emitted in this order and only when they change the state (a TLA+
// step that leaves the state unchanged is a stutter, not a transition):
//   - TMRcvPrepared(r) for each r: TM init and Prepared(r) in Msgs; add r to
//     TMPrepared;
//   - TMCommit: TM init and every RM in TMPrepared; TM committed, send Commit;
//   - TMAbort: TM init; TM aborted, send Abort;
//   - for each r: RMPrepare (working → prepared, send Prepared(r)),
//     RMChooseToAbort (working → aborted), RMRcvCommitMsg (Commit sent → r
//     committed), RMRcvAbortMsg (Abort sent → r aborted).
//
// Invariant TCConsistent: no RM is committed while another is aborted.
type TwoPhase struct{ N int } // 1 to 8 RMs

// RM and TM states.
const (
	Working uint8 = iota
	Prepared
	Committed
	Aborted
	TMInit = Working
)

// TPState is a TwoPhase state. Bit r of TMPrepared and PrepMsgs is RM r.
type TPState struct {
	N          uint8
	RM         [8]uint8
	TM         uint8
	TMPrepared uint8
	PrepMsgs   uint8 // Prepared(r) messages sent
	Commit     bool  // Commit message sent
	Abort      bool  // Abort message sent
}

func (p TwoPhase) Init(emit func(TPState) bool) {
	if p.N < 1 || p.N > 8 {
		panic(fmt.Sprintf("bench: TwoPhase needs 1-8 RMs, got %d", p.N))
	}
	emit(TPState{N: uint8(p.N)})
}

func (TwoPhase) Next(s TPState, emit func(string, TPState) bool) {
	all := uint8(1<<s.N - 1)
	if s.TM == TMInit {
		for r := range s.N {
			if bit := uint8(1) << r; s.PrepMsgs&bit != 0 && s.TMPrepared&bit == 0 {
				t := s
				t.TMPrepared |= bit
				if !emit(fmt.Sprintf("TMRcvPrepared(%d)", r), t) {
					return
				}
			}
		}
		if s.TMPrepared == all {
			t := s
			t.TM, t.Commit = Committed, true
			if !emit("TMCommit", t) {
				return
			}
		}
		t := s
		t.TM, t.Abort = Aborted, true
		if !emit("TMAbort", t) {
			return
		}
	}
	for r := range s.N {
		set := func(name string, ok bool, rm uint8, prep bool) bool {
			if !ok || s.RM[r] == rm {
				return true
			}
			t := s
			t.RM[r] = rm
			if prep {
				t.PrepMsgs |= 1 << r
			}
			return emit(fmt.Sprintf("%s(%d)", name, r), t)
		}
		w := s.RM[r] == Working
		if !set("RMPrepare", w, Prepared, true) || !set("RMChooseToAbort", w, Aborted, false) ||
			!set("RMRcvCommitMsg", s.Commit, Committed, false) || !set("RMRcvAbortMsg", s.Abort, Aborted, false) {
			return
		}
	}
}

func (TwoPhase) AppendKey(buf []byte, s TPState) []byte {
	buf = append(buf, s.N, s.TM, s.TMPrepared, s.PrepMsgs)
	buf = append(buf, s.RM[:s.N]...)
	var m byte
	if s.Commit {
		m |= 1
	}
	if s.Abort {
		m |= 2
	}
	return append(buf, m)
}

// TCConsistent is the TwoPhase invariant.
func TCConsistent(s TPState) bool {
	var committed, aborted bool
	for _, rm := range s.RM[:s.N] {
		committed = committed || rm == Committed
		aborted = aborted || rm == Aborted
	}
	return !(committed && aborted)
}
