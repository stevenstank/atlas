// Package check is the model-checking application: deterministic,
// single-threaded BFS with invariants, limits, and replayable
// counterexamples. Semantics: docs/SEMANTICS.md and DECISIONS.md D-012.
package check

import (
	"context"
	"errors"
	"runtime/metrics"
	"time"

	"github.com/stevenstank/atlas/core"
)

// Run explores m breadth-first and checks cfg.Invariants on every admitted
// state. It returns a non-nil error only for invalid configuration (wrapping
// ErrInvalidConfig), in which case no model code runs. All other outcomes,
// including model failures, are reported in Result.Status.
func Run[S, A any](ctx context.Context, m core.Model[S, A], cfg Config[S]) (Result[S, A], error) {
	e, err := newEngine(ctx, m, cfg)
	if err != nil {
		return Result[S, A]{}, err
	}
	return e.result(), nil
}

func newEngine[S, A any](ctx context.Context, m core.Model[S, A], cfg Config[S]) (*engine[S, A], error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	e := &engine[S, A]{ctx: ctx, m: m, cfg: cfg, store: core.NewStore(), k: cfg.CheckInterval}
	if e.k == 0 {
		e.k = DefaultCheckInterval
	}
	e.depthLimit, e.hasDepth = cfg.MaxDepth.Get()
	e.stateLimit, e.hasStates = cfg.MaxStates.Get()
	e.stats.MaxDepth = -1
	return e, nil
}

// result runs the exploration and attaches the statistics.
func (e *engine[S, A]) result() Result[S, A] {
	start := time.Now()
	res := e.run()
	res.Stats = e.stats
	res.Stats.WallTime = time.Since(start)
	return res
}

type item[S any] struct {
	id    core.StateID
	state S
}

// guard enforces the emit contract for one Init or Next call.
type guard struct {
	active  bool   // false once the call has returned
	stopped bool   // emit has returned false
	n       uint32 // valid emissions so far; the next one's edge ordinal
}

type engine[S, A any] struct {
	ctx   context.Context
	m     core.Model[S, A]
	cfg   Config[S]
	store *core.Store
	queue core.Queue[item[S]]
	stats Stats
	buf   []byte

	k                      int
	depthLimit, stateLimit int
	hasDepth, hasStates    bool

	inModel bool          // model code is on the stack; a panic is a ModelError
	end     *Result[S, A] // set when the run must stop
	endID   core.StateID  // the violating state, when end is a Violation
}

func (e *engine[S, A]) run() (res Result[S, A]) {
	defer func() {
		if r := recover(); r != nil {
			if !e.inModel {
				panic(r) // engine bug: do not disguise it as a model error
			}
			res = Result[S, A]{Status: ModelError, Err: &PanicError{Value: r}}
		}
	}()

	// C0: once before Init.
	if r, stop := e.interrupted(); stop {
		return Result[S, A]{Status: Incomplete, Reason: r}
	}
	g := &guard{active: true}
	e.callModel(func() { e.m.Init(func(s S) bool { return e.emitInit(g, s) }) })
	g.active = false
	if e.end != nil {
		return e.finish()
	}
	if e.stats.InitEmissions == 0 {
		return Result[S, A]{Status: ModelError, Err: ErrNoInitialStates}
	}

	for dequeues := 0; e.queue.Len() > 0; dequeues++ {
		if dequeues%e.k == 0 { // before dequeues 1, 1+K, 1+2K, ...
			if r, stop := e.interrupted(); stop {
				return Result[S, A]{Status: Incomplete, Reason: r}
			}
		}
		it := e.queue.Pop()
		e.stats.Expanded++
		depth := e.store.Depth(it.id)
		g := &guard{active: true}
		e.callModel(func() {
			e.m.Next(it.state, func(_ A, s S) bool { return e.emitNext(g, it.id, depth, s) })
		})
		g.active = false
		if e.end != nil {
			return e.finish()
		}
		if g.n == 0 {
			e.stats.TerminalStates++
		}
	}
	if e.stats.CutoffTransitions > 0 {
		return Result[S, A]{Status: Bounded, Reason: DepthLimit, limit: e.depthLimit}
	}
	return Result[S, A]{Status: Exhausted}
}

// callModel runs model code, marking it so a panic becomes a ModelError.
// On panic inModel stays set; run's recover classifies it.
func (e *engine[S, A]) callModel(f func()) {
	prev := e.inModel
	e.inModel = true
	f()
	e.inModel = prev
}

// accept validates an emit call against the contract (D-001). It returns
// false, after recording a ModelError, if the call is invalid.
func (e *engine[S, A]) accept(g *guard) bool {
	if g.active && !g.stopped {
		return true
	}
	g.stopped = true
	e.end = &Result[S, A]{Status: ModelError, Err: ErrCallbackContract}
	return false
}

// emitInit and emitNext run engine code inside a model call. They clear
// inModel on entry and restore it on return, without defer: a model panic
// below them leaves inModel set by callModel, while an engine panic leaves
// it clear and is re-raised by run.
func (e *engine[S, A]) emitInit(g *guard, s S) bool {
	e.inModel = false
	ok := e.initStep(g, s)
	e.inModel = true
	return ok
}

func (e *engine[S, A]) emitNext(g *guard, parent core.StateID, depth uint32, s S) bool {
	e.inModel = false
	ok := e.nextStep(g, parent, depth, s)
	e.inModel = true
	return ok
}

func (e *engine[S, A]) initStep(g *guard, s S) bool {
	if !e.accept(g) {
		return false
	}
	ordinal := g.n
	g.n++
	key := e.key(s)
	if _, seen := e.store.Lookup(key); seen {
		e.stats.InitEmissions++
		e.stats.InitDuplicates++
		return true
	}
	if e.hasStates && e.store.Len() >= e.stateLimit {
		e.stats.InitEmissions++
		e.refuse(g, InitPhase, 0)
		return false
	}
	if atCapacity(uint64(e.store.Len())) {
		return e.stopCapacity(g)
	}
	e.stats.InitEmissions++
	e.stats.InitAdmitted++
	return e.admit(g, s, key, core.NoParent, ordinal, 0)
}

func (e *engine[S, A]) nextStep(g *guard, parent core.StateID, depth uint32, s S) bool {
	if !e.accept(g) {
		return false
	}
	ordinal := g.n
	g.n++
	key := e.key(s)
	if _, seen := e.store.Lookup(key); seen {
		e.stats.Transitions++
		e.stats.Duplicates++
		return true
	}
	if e.hasDepth && int(depth)+1 > e.depthLimit {
		e.stats.Transitions++
		e.stats.CutoffTransitions++
		return true
	}
	if e.hasStates && e.store.Len() >= e.stateLimit {
		e.stats.Transitions++
		e.refuse(g, ExpansionPhase, int(depth)+1)
		return false
	}
	if atCapacity(uint64(e.store.Len())) {
		return e.stopCapacity(g)
	}
	e.stats.Transitions++
	return e.admit(g, s, key, parent, ordinal, depth+1)
}

// key computes the canonical key of s in the reused buffer. A panic in
// AppendKey happens before any counter is touched.
func (e *engine[S, A]) key(s S) []byte {
	e.callModel(func() { e.buf = e.m.AppendKey(e.buf[:0], s) })
	return e.buf
}

func (e *engine[S, A]) admit(g *guard, s S, key []byte, parent core.StateID, ordinal, depth uint32) bool {
	id := e.store.Add(key, parent, ordinal, depth)
	e.stats.Admitted++
	if int(depth) > e.stats.MaxDepth {
		e.stats.MaxDepth = int(depth)
	}
	if name, ok := e.violated(s); ok {
		g.stopped = true
		e.end = &Result[S, A]{Status: Violation, Violation: &Counterexample[S, A]{Invariant: name}}
		e.endID = id
		return false
	}
	e.queue.Push(item[S]{id: id, state: s})
	e.stats.FrontierPeak = max(e.stats.FrontierPeak, e.queue.Len())
	return true
}

// violated returns the first invariant, in declaration order, that s fails.
func (e *engine[S, A]) violated(s S) (string, bool) {
	for _, inv := range e.cfg.Invariants {
		ok := true
		e.callModel(func() { ok = inv.Holds(s) })
		if !ok {
			return inv.Name, true
		}
	}
	return "", false
}

func (e *engine[S, A]) refuse(g *guard, p Phase, depth int) {
	g.stopped = true
	e.stats.StateLimitRefusals = 1
	e.stats.RefusalPhase = p
	e.end = &Result[S, A]{Status: Bounded, Reason: StateLimit, limit: e.stateLimit, completeBelow: depth}
}

// atCapacity reports whether a store holding n states can admit no more.
// It compares in uint64 so that it compiles where int is 32 bits.
func atCapacity(n uint64) bool { return n >= core.MaxStoreLen }

func (e *engine[S, A]) stopCapacity(g *guard) bool {
	g.stopped = true
	e.end = &Result[S, A]{Status: Incomplete, Reason: CapacityLimit}
	return false
}

// interrupted reports whether cancellation, a deadline, or the heap limit
// stops the run. It is called only at the scheduled checks.
func (e *engine[S, A]) interrupted() (StopReason, bool) {
	if err := e.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return DeadlineExceeded, true
		}
		return Canceled, true
	}
	if e.cfg.MaxHeapBytes > 0 {
		sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		metrics.Read(sample)
		if sample[0].Value.Kind() == metrics.KindUint64 && sample[0].Value.Uint64() > e.cfg.MaxHeapBytes {
			return MemoryLimit, true
		}
	}
	return NoReason, false
}

// finish completes a run that stopped inside Init or Next.
func (e *engine[S, A]) finish() Result[S, A] {
	res := *e.end
	if res.Status != Violation {
		return res
	}
	tr, err := e.trace(e.endID)
	if err != nil {
		return Result[S, A]{Status: ModelError, Err: err}
	}
	res.Violation.Trace = tr
	return res
}
