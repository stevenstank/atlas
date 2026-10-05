package models

import (
	"fmt"
	"strings"

	"github.com/stevenstank/atlas/core"
)

// ABP is the alternating-bit protocol over two lossy, duplicating FIFO
// channels (ROADMAP Phase 2).
//
// State. The sender S has sent and had acknowledged messages m0..m(Acked-1)
// and is now sending m(Acked) with bit Acked mod 2, or is done when
// Acked == Msgs. The receiver R has delivered m0..m(Delivered-1) and expects
// bit Delivered mod 2 next. The data channel carries messages (a message's
// bit is its index mod 2, so only the index is stored); the ack channel
// carries bits. Each channel holds at most Cap entries.
//
// Actions, emitted in this order when enabled:
//   - S sends m(Acked): if not done and the data channel has room. Sending is
//     always allowed, which models retransmission after any timeout.
//   - the data channel loses or duplicates its head message (duplication
//     needs room);
//   - R receives the head message, if the ack channel has room. If its bit is
//     the expected one, R delivers it; otherwise R discards it. Either way R
//     sends an ack carrying the message's bit;
//   - the ack channel loses or duplicates its head ack;
//   - S receives the head ack. If its bit equals S's current bit, the current
//     message is acknowledged and S moves on; otherwise S ignores it.
//
// Loss and duplication act on the head only. A message lost anywhere in the
// queue is equivalent, as far as the receiver can tell, to one lost when it
// reaches the head; the abstraction only changes when capacity frees up.
//
// The broken variant, IgnoreAckBit, has S treat every ack as acknowledging
// its current message.
//
// Invariants. InOrderDelivery: R delivered m0, m1, ... in order, each once
// (OutOfOrder records the first delivery that broke this). AckedDelivered:
// every message S counts as acknowledged has been delivered (Acked <=
// Delivered). Liveness (that everything is eventually delivered) is out of
// scope.
//
// Finiteness. Acked and Delivered are at most Msgs, each channel holds at
// most Cap entries drawn from finite sets, and OutOfOrder is -1 or an index.
type ABP struct {
	Msgs         int  // messages to transfer, 1 to 255
	Cap          int  // channel capacity, 1 to 4
	IgnoreAckBit bool // the bug: S advances on any ack
}

// ABPChan is a bounded FIFO channel; Buf[Len:] stays zero.
type ABPChan struct {
	Len uint8
	Buf [4]uint8
}

func (c ABPChan) push(v uint8) ABPChan { c.Buf[c.Len] = v; c.Len++; return c }

// dupHead inserts a copy of the head in front of it.
func (c ABPChan) dupHead() ABPChan {
	copy(c.Buf[1:], c.Buf[:c.Len])
	c.Len++
	return c
}

func (c ABPChan) pop() ABPChan {
	copy(c.Buf[:], c.Buf[1:c.Len])
	c.Len--
	c.Buf[c.Len] = 0
	return c
}

// ABPState is an ABP state.
type ABPState struct {
	N                uint8 // Msgs, so the state can print itself
	Acked, Delivered uint8
	OutOfOrder       int16 // -1, or the index of the first out-of-order delivery
	Data, Acks       ABPChan
}

// ABPKind is the kind of an ABPAction.
type ABPKind uint8

const (
	Send ABPKind = iota
	LoseData
	DupData
	Deliver
	Discard
	LoseAck
	DupAck
	AckAdvance
	AckIgnore
)

// ABPAction is one ABP step. Msg is the message index for data steps and
// for AckAdvance (the message acknowledged); Bit is the bit carried.
type ABPAction struct {
	Kind     ABPKind
	Msg, Bit uint8
}

func (p ABP) Init(emit func(ABPState) bool) {
	if p.Msgs < 1 || p.Msgs > 255 || p.Cap < 1 || p.Cap > 4 {
		panic(fmt.Sprintf("models: ABP needs 1-255 messages and capacity 1-4, got %d and %d", p.Msgs, p.Cap))
	}
	emit(ABPState{N: uint8(p.Msgs), OutOfOrder: -1})
}

func (p ABP) Next(s ABPState, emit func(ABPAction, ABPState) bool) {
	full := func(c ABPChan) bool { return int(c.Len) == p.Cap }
	step := func(k ABPKind, msg, bit uint8, t ABPState) bool {
		return emit(ABPAction{Kind: k, Msg: msg, Bit: bit}, t)
	}
	if int(s.Acked) < p.Msgs && !full(s.Data) {
		t := s
		t.Data = s.Data.push(s.Acked)
		if !step(Send, s.Acked, s.Acked%2, t) {
			return
		}
	}
	if s.Data.Len > 0 {
		m := s.Data.Buf[0]
		t := s
		t.Data = s.Data.pop()
		if !step(LoseData, m, m%2, t) {
			return
		}
		if !full(s.Data) {
			t.Data = s.Data.dupHead()
			if !step(DupData, m, m%2, t) {
				return
			}
		}
		if !full(s.Acks) {
			t = s
			t.Data = s.Data.pop()
			t.Acks = s.Acks.push(m % 2)
			k := Discard
			if m%2 == s.Delivered%2 {
				k = Deliver
				if m != s.Delivered && t.OutOfOrder < 0 {
					t.OutOfOrder = int16(m)
				}
				t.Delivered++
			}
			if !step(k, m, m%2, t) {
				return
			}
		}
	}
	if s.Acks.Len > 0 {
		b := s.Acks.Buf[0]
		t := s
		t.Acks = s.Acks.pop()
		if !step(LoseAck, 0, b, t) {
			return
		}
		if !full(s.Acks) {
			t.Acks = s.Acks.dupHead()
			if !step(DupAck, 0, b, t) {
				return
			}
		}
		t = s
		t.Acks = s.Acks.pop()
		if int(s.Acked) < p.Msgs && (b == s.Acked%2 || p.IgnoreAckBit) {
			t.Acked++
			step(AckAdvance, s.Acked, b, t)
		} else {
			step(AckIgnore, 0, b, t)
		}
	}
}

func (ABP) AppendKey(buf []byte, s ABPState) []byte {
	buf = append(buf, s.N, s.Acked, s.Delivered)
	buf = core.AppendInt(buf, int64(s.OutOfOrder))
	buf = append(buf, s.Data.Len)
	buf = append(buf, s.Data.Buf[:s.Data.Len]...)
	buf = append(buf, s.Acks.Len)
	return append(buf, s.Acks.Buf[:s.Acks.Len]...)
}

// InOrderDelivery is an ABP invariant: deliveries are m0, m1, ... in order.
func InOrderDelivery(s ABPState) bool { return s.OutOfOrder < 0 }

// AckedDelivered is an ABP invariant: S never counts a message as
// acknowledged before R has delivered it.
func AckedDelivered(s ABPState) bool { return s.Acked <= s.Delivered }

func (a ABPAction) String() string {
	m := fmt.Sprintf("m%d(b%d)", a.Msg, a.Bit)
	switch a.Kind {
	case Send:
		return "S sends " + m
	case LoseData:
		return "data channel loses " + m
	case DupData:
		return "data channel duplicates " + m
	case Deliver:
		return fmt.Sprintf("R receives %s: expected bit, delivers m%d, sends ack %d", m, a.Msg, a.Bit)
	case Discard:
		return fmt.Sprintf("R receives %s: not the expected bit, discards it, sends ack %d", m, a.Bit)
	case LoseAck:
		return fmt.Sprintf("ack channel loses ack %d", a.Bit)
	case DupAck:
		return fmt.Sprintf("ack channel duplicates ack %d", a.Bit)
	case AckAdvance:
		return fmt.Sprintf("S receives ack %d: counts m%d as acknowledged, moves on", a.Bit, a.Msg)
	case AckIgnore:
		return fmt.Sprintf("S receives ack %d: ignores it", a.Bit)
	}
	return fmt.Sprintf("ABPKind(%d)", a.Kind)
}

func (s ABPState) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "S: %d acknowledged, ", s.Acked)
	if s.Acked < s.N {
		fmt.Fprintf(&b, "sending m%d(b%d)", s.Acked, s.Acked%2)
	} else {
		b.WriteString("done")
	}
	b.WriteString(" | data: [")
	for i, m := range s.Data.Buf[:s.Data.Len] {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "m%d(b%d)", m, m%2)
	}
	b.WriteString("] | acks: [")
	for i, v := range s.Acks.Buf[:s.Acks.Len] {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprint(&b, v)
	}
	fmt.Fprintf(&b, "] | R: %d delivered, expects bit %d", s.Delivered, s.Delivered%2)
	if s.OutOfOrder >= 0 {
		fmt.Fprintf(&b, " | OUT OF ORDER: delivered m%d", s.OutOfOrder)
	}
	if s.Acked > s.Delivered {
		fmt.Fprintf(&b, " | SENDER AHEAD: %d acknowledged but only %d delivered", s.Acked, s.Delivered)
	}
	return b.String()
}
