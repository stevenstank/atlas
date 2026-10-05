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
	inOrder = check.Invariant[models.ABPState]{Name: "InOrderDelivery", Holds: models.InOrderDelivery}
	acked   = check.Invariant[models.ABPState]{Name: "AckedDelivered", Holds: models.AckedDelivered}
)

func abpHolds(s models.ABPState) bool { return models.InOrderDelivery(s) && models.AckedDelivered(s) }

func runABP(t *testing.T, m models.ABP) check.Result[models.ABPState, models.ABPAction] {
	t.Helper()
	res, err := check.Run(context.Background(), m, check.Config[models.ABPState]{
		Invariants: []check.Invariant[models.ABPState]{inOrder, acked}, DetectMutation: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// The correct protocol is Exhausted at every size, with one terminal state
// (everything sent, acknowledged, and delivered; both channels empty). The
// 1-message, capacity-1 row was derived by hand (docs/CONFORMANCE.md,
// "Alternating-bit protocol"); every row is also checked against the
// reference explorer.
func TestABPCorrect(t *testing.T) {
	for _, tc := range []struct {
		msgs, cap           int
		states, transitions int64
		depth               int
	}{
		{1, 1, 9, 16, 5},
		{2, 1, 18, 36, 8},
		{2, 2, 51, 184, 10},
		{3, 2, 81, 299, 13},
		{3, 3, 172, 775, 15},
		{4, 3, 240, 1090, 18},
	} {
		m := models.ABP{Msgs: tc.msgs, Cap: tc.cap}
		res := runABP(t, m)
		s := res.Stats
		if res.Status != check.Exhausted || s.Admitted != tc.states || s.Transitions != tc.transitions ||
			s.TerminalStates != 1 || s.MaxDepth != tc.depth {
			t.Errorf("%+v: %v", m, res)
		}
		ref := reference.Explore(m, abpHolds, -1, -1)
		if ref.Status != "Exhausted" || int64(ref.Admitted) != tc.states || int64(ref.Edges) != tc.transitions {
			t.Errorf("%+v: reference explorer: %s, %d states, %d edges", m, ref.Status, ref.Admitted, ref.Edges)
		}
	}
}

// TestRegression_SenderIgnoresAckBit pins the broken sender's shortest
// counterexamples. With room for two acks, the channel duplicates ack 0 and
// the sender spends the copy on m1. With capacity 1, a retransmitted m0 makes
// the receiver send ack 0 again, with the same effect one step later.
func TestRegression_SenderIgnoresAckBit(t *testing.T) {
	dup := []string{
		"S sends m0(b0)",
		"R receives m0(b0): expected bit, delivers m0, sends ack 0",
		"ack channel duplicates ack 0",
		"S receives ack 0: counts m0 as acknowledged, moves on",
		"S receives ack 0: counts m1 as acknowledged, moves on",
	}
	retransmit := []string{
		"S sends m0(b0)",
		"R receives m0(b0): expected bit, delivers m0, sends ack 0",
		"S sends m0(b0)",
		"S receives ack 0: counts m0 as acknowledged, moves on",
		"R receives m0(b0): not the expected bit, discards it, sends ack 0",
		"S receives ack 0: counts m1 as acknowledged, moves on",
	}
	for _, tc := range []struct {
		msgs, cap int
		states    int64
		actions   []string
	}{{2, 1, 13, retransmit}, {2, 2, 22, dup}, {3, 2, 22, dup}} {
		m := models.ABP{Msgs: tc.msgs, Cap: tc.cap, IgnoreAckBit: true}
		res := runABP(t, m)
		tracetest.Expect(t, m, acked, res, tc.actions...)
		ref := reference.Explore(m, abpHolds, -1, -1)
		if res.Stats.Admitted != tc.states || ref.Status != "Violation" || int64(ref.Admitted) != tc.states || ref.ViolationDepth != len(tc.actions) {
			t.Errorf("%+v: %d states; reference %s, %d states, depth %d", m, res.Stats.Admitted, ref.Status, ref.Admitted, ref.ViolationDepth)
		}
	}
}

// No reachable state of either variant delivers out of order, so check the
// InOrderDelivery monitor directly: m2 arriving with the expected bit 0 while
// R is waiting for m0 is delivered and recorded as out of order.
func TestABPOutOfOrderMonitor(t *testing.T) {
	s := models.ABPState{N: 3, OutOfOrder: -1, Data: models.ABPChan{Len: 1, Buf: [4]uint8{2}}}
	found := false
	models.ABP{Msgs: 3, Cap: 1}.Next(s, func(a models.ABPAction, t models.ABPState) bool {
		if a.Kind == models.Deliver {
			found = t.OutOfOrder == 2 && !models.InOrderDelivery(t)
		}
		return true
	})
	if !found {
		t.Error("delivering m2 first was not recorded as out of order")
	}
}

func TestABPFormatting(t *testing.T) {
	s := models.ABPState{N: 2, Acked: 2, Delivered: 1, OutOfOrder: 1,
		Data: models.ABPChan{Len: 2, Buf: [4]uint8{1, 0}}, Acks: models.ABPChan{Len: 1, Buf: [4]uint8{1}}}
	const want = "S: 2 acknowledged, done | data: [m1(b1) m0(b0)] | acks: [1] | R: 1 delivered, expects bit 1" +
		" | OUT OF ORDER: delivered m1 | SENDER AHEAD: 2 acknowledged but only 1 delivered"
	if got := s.String(); got != want {
		t.Errorf("state:\n got %q\nwant %q", got, want)
	}
	for a, want := range map[models.ABPAction]string{
		{Kind: models.Send, Msg: 1, Bit: 1}:    "S sends m1(b1)",
		{Kind: models.LoseData, Msg: 0}:        "data channel loses m0(b0)",
		{Kind: models.DupData, Msg: 0}:         "data channel duplicates m0(b0)",
		{Kind: models.Discard, Msg: 1, Bit: 1}: "R receives m1(b1): not the expected bit, discards it, sends ack 1",
		{Kind: models.LoseAck, Bit: 1}:         "ack channel loses ack 1",
		{Kind: models.AckIgnore, Bit: 0}:       "S receives ack 0: ignores it",
		{Kind: 99}:                             "ABPKind(99)",
	} {
		if got := a.String(); got != want {
			t.Errorf("action %+v = %q, want %q", a, got, want)
		}
	}
}

func TestABPRejectsBadSize(t *testing.T) {
	if res := runABP(t, models.ABP{Msgs: 1, Cap: 5}); res.Status != check.ModelError {
		t.Errorf("status = %v, want ModelError", res.Status)
	}
}
