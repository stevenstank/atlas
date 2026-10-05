package models

import (
	"fmt"
	"strings"

	"github.com/stevenstank/atlas/core"
)

// Register is a shared read/write register used by 2 or 3 clients, each with
// a private cache (ROADMAP Phase 2).
//
// State. Memory holds the register's value. Each client has a phase (idle,
// reading, writing), the number of operations it may still start, and a
// cache that is either empty or holds a value. Every cache starts out holding
// the initial value 0, which matches memory.
//
// Operations. Each client performs at most Ops operations, one at a time.
// An operation is two steps, so other clients' steps can interleave between
// them:
//   - "starts read" / "starts write(v)" (invocation), then
//   - "read returns v" / "write(v) done" (the memory or cache access and the
//     response, one atomic step).
//
// A read returns the cached value if the cache holds one (a hit); otherwise
// it loads memory into the cache and returns that (a miss). A write stores v
// in memory and in the writer's cache and empties every other client's cache.
// The broken variant, NoInvalidate, forgets to empty the other caches.
//
// Values. Every write writes a distinct value: the j-th operation (j from 0)
// of client c, if it is a write, writes 1 + c*Ops + j. The initial value is 0.
//
// Order of actions. Clients in order A, B, C. An idle client with operations
// left emits "starts read" then "starts write(v)"; a busy client emits its
// one completing step.
//
// History. To check reads without keeping the whole history, the state keeps
// two sets of values: Completed (values whose write has finished; 0 from the
// start) and Overwritten. A value v is overwritten once some write W' that
// began after v's write finished has itself finished. Each pending operation
// records a set when it starts: a write records Completed, and when it
// finishes those values become Overwritten; a read records Overwritten.
// StaleRead is set when a read returns a value that was already overwritten
// when the read started.
//
// Invariant NoStaleRead is the real-time condition of linearizability for one
// register: a read never returns a value that was overwritten before the read
// began. Any violation is a history that is not linearizable (W(v) finishes,
// then W' runs to completion, then the read starts, so W' must come between
// W(v) and the read in every linearization, yet the read sees v). The converse
// does not hold: this is not a full linearizability check. For example, it
// does not detect two reads, both concurrent with one write, that see the new
// value and then the old one.
//
// Finiteness. Each step either starts an operation (Ops left decreases) or
// completes one, so every path has at most 2*Clients*Ops steps, and every
// field ranges over a finite set.
type Register struct {
	Clients      int  // 2 or 3
	Ops          int  // operations per client, 1 to 5
	NoInvalidate bool // the bug: writes do not empty other clients' caches
}

// RegPhase is what a client is doing.
type RegPhase uint8

const (
	RegIdle RegPhase = iota
	RegReading
	RegWriting
)

// RegClient is one client's part of a RegState.
type RegClient struct {
	Phase   RegPhase
	Left    uint8  // operations not yet started
	Cached  bool   // the cache holds a value
	Cache   uint8  // the cached value; 0 when !Cached
	Arg     uint8  // the value being written; 0 unless writing
	AtStart uint16 // set recorded when the pending operation started; 0 when idle
}

// RegState is a Register state. Value v is bit v of the uint16 sets. Unused
// client slots stay zero.
type RegState struct {
	Memory      uint8
	N           uint8 // number of clients
	C           [3]RegClient
	Completed   uint16
	Overwritten uint16
	StaleRead   bool
}

// RegAction is one step of one client.
type RegAction struct {
	Client uint8
	Start  bool // starting (true) or completing (false) an operation
	Write  bool
	Value  uint8 // written or returned value
	Hit    bool  // a read served from the cache
}

func (r Register) Init(emit func(RegState) bool) {
	if r.Clients < 2 || r.Clients > 3 || r.Ops < 1 || r.Ops > 5 {
		panic(fmt.Sprintf("models: Register needs 2-3 clients and 1-5 ops, got %d and %d", r.Clients, r.Ops))
	}
	s := RegState{N: uint8(r.Clients), Completed: 1}
	for i := range r.Clients {
		s.C[i] = RegClient{Left: uint8(r.Ops), Cached: true}
	}
	emit(s)
}

func (r Register) Next(s RegState, emit func(RegAction, RegState) bool) {
	for i := range int(s.N) {
		c := s.C[i]
		id := uint8(i)
		switch c.Phase {
		case RegIdle:
			if c.Left == 0 {
				continue
			}
			rd := s
			rd.C[i] = RegClient{Phase: RegReading, Left: c.Left - 1, Cached: c.Cached, Cache: c.Cache, AtStart: s.Overwritten}
			if !emit(RegAction{Client: id, Start: true}, rd) {
				return
			}
			v := uint8(1 + i*r.Ops + r.Ops - int(c.Left))
			wr := s
			wr.C[i] = RegClient{Phase: RegWriting, Left: c.Left - 1, Cached: c.Cached, Cache: c.Cache, Arg: v, AtStart: s.Completed}
			if !emit(RegAction{Client: id, Start: true, Write: true, Value: v}, wr) {
				return
			}
		case RegReading:
			t := s
			hit := c.Cached
			if !hit {
				c.Cached, c.Cache = true, s.Memory
			}
			if c.AtStart&(1<<c.Cache) != 0 {
				t.StaleRead = true
			}
			v := c.Cache
			c.Phase, c.AtStart = RegIdle, 0
			t.C[i] = c
			if !emit(RegAction{Client: id, Value: v, Hit: hit}, t) {
				return
			}
		case RegWriting:
			t := s
			v := c.Arg
			t.Memory = v
			t.Completed |= 1 << v
			t.Overwritten |= c.AtStart
			if !r.NoInvalidate {
				for j := range int(s.N) {
					t.C[j].Cached, t.C[j].Cache = false, 0
				}
			}
			t.C[i] = RegClient{Left: c.Left, Cached: true, Cache: v}
			if !emit(RegAction{Client: id, Write: true, Value: v}, t) {
				return
			}
		}
	}
}

func (Register) AppendKey(buf []byte, s RegState) []byte {
	buf = append(buf, s.Memory, s.N)
	buf = core.AppendUint(buf, uint64(s.Completed))
	buf = core.AppendUint(buf, uint64(s.Overwritten))
	buf = core.AppendBool(buf, s.StaleRead)
	for _, c := range s.C[:s.N] {
		buf = append(buf, byte(c.Phase), c.Left, c.Cache, c.Arg)
		buf = core.AppendBool(buf, c.Cached)
		buf = core.AppendUint(buf, uint64(c.AtStart))
	}
	return buf
}

// NoStaleRead is the Register invariant: no read has returned a value that
// was overwritten before the read started.
func NoStaleRead(s RegState) bool { return !s.StaleRead }

func clientName(i uint8) string { return string(rune('A' + i)) }

func (a RegAction) String() string {
	n := clientName(a.Client)
	switch {
	case a.Start && a.Write:
		return fmt.Sprintf("%s starts write(%d)", n, a.Value)
	case a.Start:
		return n + " starts read"
	case a.Write:
		return fmt.Sprintf("%s write(%d) done: memory = %d", n, a.Value, a.Value)
	case a.Hit:
		return fmt.Sprintf("%s read returns %d (cache hit)", n, a.Value)
	}
	return fmt.Sprintf("%s read returns %d (cache miss, loaded from memory)", n, a.Value)
}

func (s RegState) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "memory=%d", s.Memory)
	for i, c := range s.C[:s.N] {
		fmt.Fprintf(&b, " | %s ", clientName(uint8(i)))
		switch c.Phase {
		case RegIdle:
			b.WriteString("idle")
		case RegReading:
			b.WriteString("reading")
		case RegWriting:
			fmt.Fprintf(&b, "writing %d", c.Arg)
		}
		if c.Cached {
			fmt.Fprintf(&b, ", cache=%d", c.Cache)
		} else {
			b.WriteString(", cache empty")
		}
	}
	if s.Overwritten != 0 {
		fmt.Fprintf(&b, " | overwritten: %s", valueSet(s.Overwritten))
	}
	if s.StaleRead {
		b.WriteString(" | STALE READ: a read returned a value overwritten before it started")
	}
	return b.String()
}

func valueSet(m uint16) string {
	var vs []string
	for v := range 16 {
		if m&(1<<v) != 0 {
			vs = append(vs, fmt.Sprint(v))
		}
	}
	return "{" + strings.Join(vs, ",") + "}"
}
