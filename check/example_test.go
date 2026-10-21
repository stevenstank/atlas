package check_test

import (
	"context"
	"fmt"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/core"
)

// Counter is two processes that each add 1 to a shared counter. Unless
// Atomic, an increment is two steps, a read into a private register and a
// write of that register plus 1, so the other process can run in between.
type Counter struct{ Atomic bool }

// CounterState is the shared counter, each process's program counter
// (0: not started, 1: has read, 2: done), and each private register.
type CounterState struct {
	Counter int
	PC      [2]int
	Reg     [2]int
}

func (m Counter) Init(emit func(CounterState) bool) { emit(CounterState{}) }

func (m Counter) Next(s CounterState, emit func(string, CounterState) bool) {
	for p := range 2 {
		t := s // CounterState is a value, so t is a copy
		switch {
		case s.PC[p] == 0 && m.Atomic:
			t.Counter, t.PC[p] = s.Counter+1, 2
			if !emit(fmt.Sprintf("P%d increments to %d", p, t.Counter), t) {
				return
			}
		case s.PC[p] == 0:
			t.Reg[p], t.PC[p] = s.Counter, 1
			if !emit(fmt.Sprintf("P%d reads %d", p, s.Counter), t) {
				return
			}
		case s.PC[p] == 1:
			t.Counter, t.PC[p] = s.Reg[p]+1, 2
			if !emit(fmt.Sprintf("P%d writes %d", p, t.Counter), t) {
				return
			}
		}
	}
}

func (m Counter) AppendKey(buf []byte, s CounterState) []byte {
	buf = core.AppendInt(buf, int64(s.Counter))
	for p := range 2 {
		buf = core.AppendInt(buf, int64(s.PC[p]))
		buf = core.AppendInt(buf, int64(s.Reg[p]))
	}
	return buf
}

// noLostUpdate holds when, once both processes are done, the counter is 2.
var noLostUpdate = check.Invariant[CounterState]{
	Name:  "no lost update",
	Holds: func(s CounterState) bool { return s.PC != [2]int{2, 2} || s.Counter == 2 },
}

// Run explores every interleaving breadth-first, so the counterexample is a
// shortest one: both processes read 0 before either writes.
func ExampleRun() {
	res, err := check.Run(context.Background(), Counter{},
		check.Config[CounterState]{Invariants: []check.Invariant[CounterState]{noLostUpdate}})
	if err != nil {
		panic(err) // only an invalid Config
	}
	fmt.Print(res)
	// Output:
	// Violation: invariant "no lost update" is violated; counterexample has 4 steps
	// counterexample: invariant "no lost update" fails after 4 steps
	//   step 0: initial state
	//           {Counter:0 PC:[0 0] Reg:[0 0]}
	//   step 1: P0 reads 0
	//           {Counter:0 PC:[1 0] Reg:[0 0]}
	//   step 2: P1 reads 0
	//           {Counter:0 PC:[1 1] Reg:[0 0]}
	//   step 3: P0 writes 1
	//           {Counter:1 PC:[2 1] Reg:[0 0]}
	//   step 4: P1 writes 1
	//           {Counter:1 PC:[2 2] Reg:[0 0]}
	//   the state after step 4 violates "no lost update"
	// states 12, transitions 12, terminal states 0, max depth 4
}

// With atomic increments, every reachable state satisfies the invariant, and
// the Exhausted result is a proof over the whole model.
func ExampleRun_exhausted() {
	res, err := check.Run(context.Background(), Counter{Atomic: true},
		check.Config[CounterState]{Invariants: []check.Invariant[CounterState]{noLostUpdate}})
	if err != nil {
		panic(err)
	}
	fmt.Print(res)
	// Output:
	// Exhausted: verified: no reachable state violates any invariant
	// states 4, transitions 4, terminal states 1, max depth 2
}
