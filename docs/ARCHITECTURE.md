# Architecture

> Status: **proposed**. No code exists yet. All Go signatures below are
> illustrative and become final only when the matching record in
> [DECISIONS.md](DECISIONS.md) is resolved. Performance-related choices here
> are starting points to be measured in Phase 3, not settled facts.

## 1. Layers

```
            ┌──────────────────────┐      ┌───────────────────────┐
            │  check  (App A)      │      │  optimize  (App B,    │
            │  invariants, BFS     │      │  Phase 5) constraints,│
            │  verdicts, traces    │      │  objective, B&B       │
            └──────────┬───────────┘      └───────────┬───────────┘
                       │  uses                         │ uses
            ┌──────────▼──────────────────────────────▼───────────┐
            │  core (engine)                                      │
            │  model contract · state store/dedup · frontier      │
            │  limits & cancellation · stats · trace replay       │
            └─────────────────────────────────────────────────────┘
            ┌─────────────────────────────────────────────────────┐
            │  models/ (reference & benchmark models)  · bench/   │
            └─────────────────────────────────────────────────────┘
```

- **core** knows nothing about invariants or objectives. It provides state
  identity, storage, a frontier, limits, statistics, and path replay.
- **check** owns search order (BFS), invariant evaluation, result status, and
  counterexamples.
- **optimize** owns its own search loop and result semantics (Phase 5, see
  [OPTIMIZATION.md](OPTIMIZATION.md)).
- Proposed package layout (D-011): `core/`, `check/`, `optimize/`, `models/`,
  `internal/bench/`. Atlas is library-first. A CLI is deferred until a need
  for one is shown.

## 2. Data flow (model checking, BFS)

```
Init ──► for each s0: key=Key(s0) ─► visited.Insert(key) ─new─► check invariants ─► frontier.Push
                                                    └─dup─► ignore

loop until frontier empty / limit / violation:
   (id, s) = frontier.Pop()
   Next(s, emit) ── for each (action, t) in order:
        stats.transitions++
        key = Key(t)
        visited.Insert(key, parent=id, edge=ordinal) ─dup─► stats.duplicates++
                                                     └new─► check invariants(t)
                                                              ├ fail ─► stop, build trace(newID)
                                                              └ ok ───► frontier.Push(newID, t)
```

## 3. Components

### 3.1 Model contract

**Responsibility:** user code that defines the state space (SEMANTICS.md §1).

Shape accepted in D-001: **option 3b** (2026-10-04). The alternatives that
were not chosen (3a with no `bool`, and 4 with `iter.Seq2`) are recorded in
D-001.

```go
// D-001 option 3b (accepted). S is the state type, A the action label type.
type Model[S any, A any] interface {
    // Init emits initial states in a deterministic order. If emit returns
    // false, Init must make no further emit calls and return promptly.
    Init(emit func(S) bool)
    // Next emits (action, successor) pairs for s in a deterministic order,
    // with the same stop rule. emit may be called only synchronously, from
    // this goroutine, during this call. It must not mutate s or retain
    // emitted states for mutation.
    Next(s S, emit func(A, S) bool)
    // AppendKey appends the canonical encoding of s to buf and returns it.
    AppendKey(buf []byte, s S) []byte
}
```

Invariants are passed to the checker, not to the model:
`check.Invariant[S]{Name string; Holds func(S) bool}`.

**Why callbacks (`emit`) rather than returning slices:** the model does not
have to allocate a slice for every state. Under no option can the engine
*interrupt* a running `Init` or `Next` call. Whether it can at least *ask* the
model to stop, at a violation or a limit, depends on D-001. With option 3a as
shown it cannot ask: the engine only discards the remaining emissions until
the call returns. Under options 3b (`emit` returns `bool`) and 4
(`iter.Seq2`), it signals the stop, and a model honoring the contract returns
promptly. The cost of callbacks is that user code is slightly less natural to
write.

**Ownership:** once a state is emitted, it belongs to the engine and must not
be mutated by anyone. States containing slices or maps must be copied by
`Next` before modification. Copy-on-write is the model's responsibility. Tests
include an aliasing detector (TESTING.md §3).

### 3.2 State identity and the visited set

**Responsibility:** decide whether a state is new and assign it a dense
`StateID uint32` (or `uint64` if 4G states becomes reachable).

Baseline (D-002, D-003): key bytes come from `AppendKey` into a reused buffer.
The visited map is `map[string]StateID`. Go's map hashes the key and compares
full strings, so identity is exact and collisions are handled correctly with no
extra code. The string conversion copies the key once, for new states only,
when the key is stored (Go does not allocate for a map *lookup* with
`m[string(b)]`).

Per-state metadata lives in parallel slices indexed by `StateID`:

```go
parent []StateID   // NoParent for initial states
edge   []uint32    // ordinal of the step in parent's Next emission (or Init index)
depth  []uint32    // needed for depth bounds and MaxDepth (a per-level counter would also work)
```

Alternatives to measure in Phase 4 (not decided):

| Option                             | Exact? | Memory/state (est., *unmeasured*)  | Notes |
|------------------------------------|--------|------------------------------------|-------|
| `map[string]StateID` (baseline)    | yes    | key + ~string header + map overhead| simplest; GC scans string pointers |
| Arena of key bytes + open-addressing table of (hash, offset) | yes | key + ~12–16 B | far fewer GC pointers; needs custom code |
| Fixed-width keys packed in `[]uint64` | yes | key only + table | only for models whose keys fit a known width |
| Fingerprint-only set (TLC-style)   | **no** | 8 B                                | probabilistic; opt-in only, results labeled |

### 3.3 Frontier

**Responsibility:** hold discovered states that have not been expanded yet.

Baseline: a FIFO of `(StateID, S)` stored in a growable ring buffer or a
chunked queue. It holds the live `S` values so the engine never needs a
`Decode` function. The visited set keeps only keys.

Alternative (Phase 4): store only `StateID` and recover `S` by decoding the
stored key. This needs a model-supplied `Decode`. It saves memory when `S` is
much larger than its key, at the cost of a decode per expansion. D-003.

### 3.4 Search algorithms

- **BFS** (Phase 1): the only algorithm with the shortest-trace guarantee.
- **DFS** (later, if measured to be needed): less frontier memory, no
  shortest-trace guarantee. Results must say so.
- **Random simulation** (not planned): would have to report `Incomplete`
  always.

The search loop lives in `check`, not `core`, because the order in which the
optimizer expands states differs (D-004, D-009).

### 3.5 Invariant checking

Invariants are evaluated on discovery, in declaration order. Panics are caught
with `recover` at the call boundary and turned into `ModelError` with the
state ID, so the state can be reported. SEMANTICS.md §5.

### 3.6 Counterexample reconstruction

The engine stores no states and no actions per discovered state, only
`(parent, edge)`. To build a trace for state `v`:

1. Walk `parent` from `v` to the root, collecting edge ordinals, then reverse
   them.
2. Re-run `Init` and take the `edge[root]`-th emitted state.
3. For each subsequent ordinal k, call `Next` on the current state and take
   the k-th emitted `(action, successor)`.
4. After each step, check that `Key(successor)` equals the stored key for that
   ID. A mismatch means the model is nondeterministic: report `ModelError`.

Cost: O(trace length × branching) extra calls to `Next`, paid only when a trace
is built. Memory: about 8 bytes per state. The alternative, storing the action
and state for every discovered state, multiplies memory to save work that
happens only once (D-005).

```go
// Proposed result shape.
type Trace[S, A any] struct {
    Steps []Step[S, A] // Steps[0] has the zero Action and the initial state
}
type Step[S, A any] struct { Action A; State S }
```

### 3.7 Result reporting

```go
// Proposed.
type Status int // Exhausted, Bounded, Incomplete, Violation, ModelError

type Result[S, A any] struct {
    Status    Status
    Reason    StopReason      // DepthBound, StateBound, Timeout, MemoryLimit, Canceled, ...
    Claim     string          // human-readable statement of exactly what was shown
    Violation *Violation[S,A] // invariant name + Trace, when Status == Violation
    Err       error           // when Status == ModelError
    Stats     Stats
}
```

There is deliberately no `OK bool` field. A caller has to switch on `Status`.
SEMANTICS.md §8.

### 3.8 Statistics and instrumentation

The counters from SEMANTICS.md §10 are plain integer fields, incremented
inline. That costs essentially nothing single-threaded. Memory statistics come
from `runtime/metrics`, sampled at the same points as the limit checks.
Optional progress reporting goes through a callback,
`Progress func(Stats)`, invoked at most every T. No logging library is used.

### 3.9 Cancellation and resource limits

`Run(ctx, model, opts)`. The engine checks `ctx.Err()`, the deadline, and
memory at fixed points. *(Schedule per DECISIONS.md D-012, "Interruption
check schedule".)* The checks happen once before `Init`,
then before dequeues 1, 1+K, 1+2K, …. They never happen inside `Init` or
`Next`, and there is no check once the frontier is empty. K is tunable, starts
around 1024, and will be measured. Tests that need deterministic
interruption use K = 1.
Limits:

| Limit         | Mechanism                             | Exactness |
|---------------|---------------------------------------|-----------|
| Depth bound   | don't expand states at depth = D *(superseded by D-012: depth-D states are checked and expanded; unseen D+1 successors are refused as `CutoffTransitions`)* | exact |
| State bound   | stop after N discoveries *(superseded by D-012: N includes the initial state; stop only when an unseen in-depth state would exceed N. The depth check runs before the state check.)* | exact |
| Time limit    | context deadline                      | approximate, checked every K |
| Memory limit  | `runtime/metrics` heap sample vs. cap | approximate; may overshoot |

`debug.SetMemoryLimit` is a GC soft target, not a hard cap. It may be set to
help the GC, but it is not what enforces the limit.

### 3.10 Benchmarking and profiling hooks

- Standard `testing.B` benchmarks per model in `internal/bench`, reporting
  custom metrics (`states/s`, `B/state`) with `b.ReportMetric`.
- `go test -cpuprofile/-memprofile`, plus `runtime/pprof` hooks in an optional
  benchmark driver.
- Every benchmark checks the expected state count or verdict and fails if it
  differs ([BENCHMARKS.md](BENCHMARKS.md)).

## 4. Boundary between core, check, and optimize

| Concern                        | core | check | optimize |
|--------------------------------|:----:|:-----:|:--------:|
| Model / generator contract     |  ✓   |       |          |
| Key encoding, visited set, IDs |  ✓   |       |  reuse   |
| Frontier data structures       |  ✓   | FIFO  | stack/heap |
| Limits, cancellation, stats    |  ✓   |       |          |
| Path replay from (parent,edge) |  ✓   | traces| solution paths |
| Invariants, Violation status   |      |  ✓    |          |
| Objective, bounds, incumbent   |      |       |  ✓       |

Rule: nothing optimization-specific (objectives, bounds, incumbents) goes into
`core` or `check`. If `optimize` needs something new from `core`, it is added
only when its use is shown to be general (D-009).

## 5. Parallelism (future, Phase 6)

Not part of the baseline. A parallel BFS needs a concurrent visited set and
level-synchronous expansion to keep depths exact. Even then, the parent chosen
for a state, and therefore the reported trace, can depend on scheduling. A
parallel mode must either (a) synchronize by level and break ties
deterministically, for example lowest (parent ID, edge), to keep both the
guarantee and determinism, or (b) document a weaker contract. D-008.
