# Decision records

Lightweight ADRs. Each record has a status:

- **Proposed:** a recommendation exists, but the owner has not accepted it.
- **Accepted:** agreed, and code may depend on it.
- **Unresolved:** more evidence is needed before deciding.
- **Superseded:** replaced by a later record (linked).

All records below are **Proposed** or **Unresolved** as of 2026-10-04. None is
accepted yet. Phase 0 exits when D-001 through D-006, D-011, and D-012 are
accepted ([ROADMAP.md](ROADMAP.md)).

## Phase 0 review summary (awaiting owner decision)

**Accepted: none.** Every row below is *Proposed* and needs an explicit owner
decision: accept, revise, or reject. D-007 to D-010 do not block Phase 1 and
are not listed.

| ID | Proposed decision | Rationale | Alternatives considered | Key consequences |
|----|-------------------|-----------|-------------------------|------------------|
| D-001 | `Model[S, A]` with `Init(emit)`, `Next(s, emit func(A, S))`, `AppendKey(buf, s)`. Emitted states are immutable and owned by the engine. | Action labels for traces; no slice allocated per state; the engine can stop partway through a state's successors (but see the open point in D-001). | `Next(s) []S`; `Actions` + `NextState` (Stateright); `iter.Seq2`. | Callback style is less familiar. Models must copy before mutating. Model types are not interchangeable behind one interface value. |
| D-002 | Identity = exact canonical byte key. No fingerprint-only mode in Phases 1–4. | Exact verdicts. Hash collisions cannot cause missed states. Works for any state shape. | `S comparable` with `map[S]ID`; 64-bit fingerprints (TLC-style). | Cost of encoding every successor. The model author must make the key injective and canonical. Helper encoders are needed. |
| D-003 | Baseline: `map[string]StateID` + `parent`/`edge`/`depth` slices + FIFO of `(ID, S)`. Optimized layout deferred. | Simplest correct design, using the map's built-in collision handling. Measure before redesigning. | Arena + open addressing; packed fixed-width keys; frontier stores IDs and decodes. | Higher memory per state and GC pressure, both known up front. Phase 4 may replace it, behind the same semantics. |
| D-004 | Single-threaded BFS only in Phase 1. DFS later, only if measured to be needed. | BFS alone guarantees shortest traces. | DFS; iterative deepening; random simulation. | Wide models may exhaust memory in the frontier. |
| D-005 | Store `(parent, edge ordinal)` per state; rebuild traces by replaying `Next`. | About 8 B/state. Replay also detects nondeterminism. | Store `(parent, action)`; store full states. | Trace building costs extra `Next` calls. Requires deterministic models, which D-006 requires anyway. |
| D-006 | Pure model + same config + single-threaded BFS ⇒ identical verdict, trace, and counted stats. Order comes only from `Init`/`Next`. | Reproducible bugs and regression tests; replay depends on it. | Weaker contract (verdict only); seeded randomized order. | Constrains parallelism (D-008) and any randomized data structure. Timing-cut runs are excluded from the contract. |
| D-011 | Module `github.com/stevenstank/atlas` (**assumes** that GitHub location); `go 1.26`; packages `core`, `check`, `models`, `internal/bench`. | One module keeps things simple. The directive matches the tested toolchain. | Another host or path; a lower directive such as `go 1.23` for wider compatibility. | The path is costly to change after imports exist. The directive sets the minimum Go version for every user. |
| D-012 | Depth limit D = maximum admitted depth. Depth-D states are expanded; unseen successors are refused as `CutoffTransitions`. A state limit N counts the initial state and stops only when an unseen state would exceed N. `Bounded` only if something was refused; violations beyond a bound are not reported as found; cancel/timeout/resources ⇒ `Incomplete`. | `Bounded` is never spurious; limits that don't matter yield `Exhausted`; fixes a rule that cannot be implemented. | Don't expand depth-D states (always `Bounded`); admit depth D+1 without expanding; stop at the N-th state. | One extra BFS level of `Next` calls under a depth limit. New `CutoffTransitions` statistic. SEMANTICS/ARCHITECTURE/TESTING edits on acceptance. |

---

## D-001 Model interface and state ownership — Proposed

**Context.** The interface determines how easy models are to write, how much
the engine allocates, and whether traces carry readable action labels.

**Options.**
1. `Next(s) []S`: simple, but allocates a slice per state and has no action
   labels.
2. `Actions(s) []A` + `NextState(s, a) S`, as in Stateright: readable labels,
   but two calls and a slice per state.
3. `Next(s S, emit func(A, S))`: labels, no slice, and the engine can stop
   early.
4. `Next(s S) iter.Seq2[A, S]`: like 3, using Go 1.23 iterators. Feels
   idiomatic, but each call may allocate a closure.

**Recommendation.** Option 3, with generic `Model[S, A any]` and
`AppendKey(buf, s) []byte` (ARCHITECTURE.md §3.1). Emitted states are
immutable and owned by the engine.

**Trade-offs.** Callbacks are less familiar to new Go users than returned
slices. Generics rule out mixing different model types behind one interface
value, which no current requirement needs.

**Open point (found in the 2026-10-04 consistency review).** With
`emit func(A, S)`, the engine cannot tell `Next` to stop. When a violation or
a state bound ends the run partway through a state's successors, the engine
can only ignore the remaining emissions, and the model still computes them.
Semantics are unaffected, because the ignored steps are not counted (SEMANTICS.md
§10), but the claimed early-stop benefit does not exist. Fix options:
`emit func(A, S) bool`, where `false` means stop (this matches `iter.Seq2`
yield semantics and puts an obligation on the model), or keep the signature
and accept the wasted work. **Suggestion:** return `bool`. Not applied,
pending owner review.

**Validation.** Write the Phase 0 tiny model (`Grid2`) and one protocol model
in options 3 and 4, and compare readability and allocations per expansion with
`testing.AllocsPerRun`.

## D-002 State identity and canonicalization — Proposed

**Context.** Identity must be exact (SEMANTICS.md §3). Canonical form is the
model author's responsibility, and getting it wrong in one direction silently
loses behaviors.

**Options.**
1. `S comparable`, with identity by Go `==` and storage in `map[S]ID`. Easy,
   but forbids slices and maps in states, and `==` on pointers compares
   addresses, which is a trap.
2. A model-supplied canonical byte key (`AppendKey`), where identity is byte
   equality.
3. Fingerprint only (64-bit hash, TLC-style). Least memory, but probabilistic.

**Recommendation.** Option 2 as the only mode in Phases 1–4. Provide helper
encoders, such as varint and length-prefixed append helpers, so canonical keys
are easy to write. Option 3 may come later only as an explicit opt-in whose
results are labeled `Probabilistic`, along with a reported collision bound.

**Trade-offs.** Encoding costs CPU on every successor, including duplicates.
Phase 3 measures how much.

**Validation.** Hash-collision tests with a deliberately constant hash in a
test build (TESTING.md §3). Key-injectivity property tests for every
reference model.

## D-003 Visited set and frontier design — Proposed (baseline), Unresolved (optimized)

**Context.** These two structures dominate memory and are likely CPU hot spots
(unmeasured).

**Options.** See ARCHITECTURE.md §3.2–3.3. Visited: a Go map, an arena with
open addressing, or packed fixed-width keys. Frontier: live `S` values, or IDs
plus decode.

**Recommendation.** Baseline: `map[string]StateID`, parallel `parent`/`edge`
slices, and a FIFO of `(ID, S)`. Defer the optimized design until Phase 3
profiles exist.

**Trade-offs.** The baseline puts GC pressure on string pointers and uses more
memory per state than custom tables. It is accepted for simplicity and
correctness.

**Validation.** The Phase 3 baseline measurements, and Phase 4 A/B tests on
the fixed suite with identical semantic results.

## D-004 Search algorithm selection — Proposed

**Recommendation.** Single-threaded BFS is the only algorithm in Phase 1. DFS
is added only if Phase 3 shows the frontier is the memory bottleneck on real
models. Its results must not claim shortest traces.

**Trade-offs.** BFS frontiers can be very large on wide models.

**Validation.** Shortest-trace tests against the reference explorer
(TESTING.md §5).

## D-005 Counterexample representation — Proposed

**Options.**
1. Store `(parent, edge ordinal)` per state, and rebuild the trace by
   replaying `Next`.
2. Store `(parent, action)` per state.
3. Store full states per ID.

**Recommendation.** Option 1, about 8 B/state. Replay also detects
nondeterminism.

**Trade-offs.** Rebuilding a trace calls `Next` again about (depth × branching)
times. That is negligible for one trace, but it requires determinism, which
the engine needs anyway.

**Validation.** Every counterexample in every test is replayed independently
and checked to reach the violating key.

## D-006 Determinism guarantees — Proposed

**Recommendation.** The contract in SEMANTICS.md §7: identical verdicts,
traces, and counted statistics for pure models under single-threaded BFS.
Order comes only from `Init`/`Next` emission order. The engine never lets map
iteration order affect exploration.

**Trade-offs.** This constrains future parallelism (D-008) and any use of
randomized data structures.

**Validation.** Run each reference model N times, and under different
`GOMAXPROCS` values, and compare results byte for byte.

## D-007 Benchmark baselines — Proposed

**Context.** Comparisons are needed (a) between Atlas versions and (b) with
other tools.

**Recommendation.** The primary baseline is Atlas's own Phase 3 engine on the
fixed suite ([BENCHMARKS.md](BENCHMARKS.md)). External comparisons are
optional. They need equivalent models whose state counts match exactly, and
the methodology must be published.

**Validation.** `benchstat` over ≥10 runs. State counts are checked on every
run.

## D-008 Future parallelism — Unresolved

**Context.** Parallelism can change discovery order, parent choice, and
therefore traces and the shortest-trace guarantee (SEMANTICS.md §6).

**Options.**
1. Level-synchronous parallel BFS with deterministic tie-breaking
   (lowest parent ID, then edge). Keeps the guarantee, at the cost of a
   barrier per level.
2. Asynchronous work-stealing. Fastest in theory, but traces may not be
   shortest and runs are not deterministic.
3. Parallel successor generation only, with single-threaded insertion.

**Recommendation.** None yet. Revisit in Phase 6 with profiles. Whatever is
chosen must state its guarantees in SEMANTICS.md.

**Validation.** State counts must match the sequential run exactly. Trace
lengths must match the sequential run if the guarantee is claimed. Use
`-race`.

## D-009 How optimization shares the core — Proposed

**Recommendation.** `optimize` reuses `core` (generator contract, keys, visited
set, limits, stats, path replay) and implements its own search loop and result
type. It does **not** reuse `check`'s BFS loop or `Status` values
(OPTIMIZATION.md §6).

**Trade-offs.** Some loop logic is duplicated, in exchange for no
optimization-specific branches in the model checker.

**Validation.** Before Phase 5 begins, review whether `core` has gained any
import of, or concept from, `optimize`.

## D-010 Deadlock (terminal state) default — Unresolved

**Options.** Treat terminal states as violations by default (as TLC does), or
treat them as allowed by default with an opt-in check.

**Recommendation (tentative).** Allowed by default, and always counted in the
statistics. Many simple reference models end in legitimate terminal states.

**Trade-offs.** Users who expect TLC's behavior may miss deadlocks. This is
mitigated by printing the terminal-state count in every result.

**Validation.** Owner decision. A test model with a deliberate deadlock is
needed in either case.

## D-011 Module path, Go directive, and package layout — Proposed (awaiting owner approval)

**Context.** No `go.mod` exists. Two values must be fixed before Phase 1
creates it.

**Proposed module path: `github.com/stevenstank/atlas`.**
⚠ *Assumption to approve:* the repository will be published at
`https://github.com/stevenstank/atlas`. If it is hosted elsewhere, or renamed,
the path must change before any package imports it. Changing it afterward
means rewriting every import.

**Proposed Go directive: `go 1.26`.** This is a **compatibility decision**,
not a record of which toolchain happens to be installed locally. The `go`
line in `go.mod`:

- sets the **minimum** Go version needed to build the module. Since Go 1.21
  it is enforced: an older toolchain refuses to build, or, depending on
  `GOTOOLCHAIN`, downloads a newer one. Every user of the Atlas library
  inherits this minimum;
- selects **language semantics** for the module. For example, per-iteration
  loop variables apply only when the directive is ≥ 1.22.

Features Atlas is expected to need: generics (1.18), `min`/`max` builtins
(1.21), per-iteration loop variables (1.22), and, only if D-001 picks
`iter.Seq2`, range-over-func iterators (1.23). A lower directive such as
`go 1.23` would therefore work for the planned code and accept more
toolchains. `go 1.26` keeps the build matched to what development and CI
actually test. The Go project supports the two most recent major releases,
so check <https://go.dev/doc/devel/release> when deciding. The
recommendation is `go 1.26` because nothing is gained by claiming
compatibility that is never tested. If you want wider reach, choose the lower
version and add a CI job that builds with it. No separate `toolchain` line is
proposed.

**Package layout.** One module. Packages: `core`, `check`, `models`,
`internal/bench`, and later `optimize`. No public `cmd/` until a CLI is
justified. The one-shot benchmark driver proposed in
[BENCHMARKS.md](BENCHMARKS.md) §5 lives under `internal/` and is not a public
CLI.

**Validation.** The owner approves the module path and the `go` directive
before Phase 1 creates `go.mod`.

## D-012 Completion status under depth and state bounds — Proposed (awaiting owner approval)

### Problem

SEMANTICS.md §8 rule 2 says states at depth D are "not expanded", and also
that the run is `Exhausted` if "no state at depth D has any successor". The
engine cannot know whether a state has successors without calling `Next`, so
the rule **cannot be implemented as written**. It also contradicts other
documents:

- SEMANTICS.md §8 (Grid2 with bound 4 ⇒ `Exhausted`) and TESTING.md §7
  (bound ≥ max depth ⇒ `Exhausted`) both assume the depth-D states *are*
  expanded.
- ARCHITECTURE.md §3.9 says "don't expand states at depth = D".
- Rule 3 (state bound N) does not say what happens when the model has exactly
  N reachable states.

### Guiding principle

**`Bounded` must be backed by evidence.** A run is `Bounded` only if the
engine generated at least one successor that is reachable and previously
unseen, and refused it because of a limit. Reaching a limit is not, by
itself, proof that unexplored states remain.

### Options considered

| | Depth-D states | Successors beyond the bound | Bound equal to the true size | Cost |
|---|---|---|---|---|
| (a) | not expanded | never generated | `Bounded` (cannot know otherwise) | none |
| **(b)** | **expanded** | **looked up; unseen ones refused and counted as `CutoffTransitions`** | **`Exhausted`** | one extra level of `Next` + key lookups |
| (c) | expanded | unseen ones admitted and checked, not expanded | `Exhausted` | the claim becomes "≤ D, plus some of D+1", which is confusing |

State bound: (i) stop as soon as the N-th state is admitted and report
`Bounded`, or **(ii) stop only when an unseen state would exceed N.**

Proposed: **(b) + (ii)**.

### Proposed semantics (replacement text for SEMANTICS.md §8 rules 2–3, not yet applied)

> **Depth limit D (D ≥ 0).**
> 1. D is the maximum depth of states that may be admitted to the visited
>    set and checked against the invariants.
> 2. States at depth D are expanded, to detect their successors.
> 3. A successor that was already discovered is counted as a duplicate, as
>    usual.
> 4. A previously unseen successor of a depth-D state lies beyond the limit.
>    It is not admitted, not checked, and not enqueued. Each such transition
>    counts as one `CutoffTransition`.
> 5. After every boundary state has been examined (the frontier is empty),
>    the result is `Bounded` if `CutoffTransitions > 0` and `Exhausted`
>    otherwise.
>
> **State limit N (N ≥ 1).**
> 1. N counts every admitted state, including initial states.
> 2. When a previously unseen state would make the count N + 1, it is not
>    admitted or checked. It counts as one `CutoffTransition`, and the run
>    returns `Bounded` immediately.
> 3. Reaching exactly N states does not by itself imply `Bounded`. If the
>    frontier then empties without another unseen state appearing, the result
>    is `Exhausted`.
>
> **Violations.** A violation in an admitted state, that is, within the
> explored bounds, returns `Violation`. A violating state beyond a bound is
> never admitted or checked, so it is not reported as found, and the run
> returns `Bounded`.
>
> **Cancellation, timeout, resource exhaustion.** If one of these stops the
> run before it can reach a conclusion (the frontier is non-empty and no
> violation was found), the result is `Incomplete`. It does not claim that
> unexplored states exist, only that the question was not settled. These
> limits are checked before the next state is dequeued. If the frontier is
> already empty, the run is `Exhausted` or `Bounded` under the rules above.
>
> **Claims.** A `Bounded` run caused by a depth limit claims "no invariant
> violation in any state at depth ≤ D". A `Bounded` run caused by a state
> limit claims "no violation among the N admitted states" and reports the
> depth d below which BFS was complete.

**Statistic.** Add `CutoffTransitions`, which counts *transitions* refused by
a limit, not distinct states. Two refused transitions may lead to the same
state (Grid2 depth 3 below). The identity in SEMANTICS.md §10 becomes:
`transitions = (discovered − |distinct init|) + duplicates + cutoffTransitions`.

### Why (b) is sound

BFS admits every depth-D state before expanding any depth-D state. So when a
depth-D state is expanded, every state at depth ≤ D is already in the
visited set, and a successor that is not in the set has true BFS depth
exactly D + 1. Each cutoff transition therefore points at a real, reachable,
unexplored state, which is the evidence `Bounded` requires. If no cutoff
occurs, every successor of every admitted state was already admitted. The
visited set is then closed under `Next`, so it equals the whole reachable
set, and `Exhausted` is correct.

### Boundary cases and expected outcomes

Grid2 (SEMANTICS.md §6, maximum depth 4, 9 states). All rows were verified
with a throwaway script:

| Config | Status | Disc. | Exp. | Tr | Dup | Cutoff | Why |
|---|---|---:|---:|---:|---:|---:|---|
| depth 3 | `Bounded` | 8 | 8 | 12 | 3 | 2 | (2,1)→(2,2) and (1,2)→(2,2): one state, two cutoff transitions |
| depth 4 | `Exhausted` | 9 | 9 | 12 | 4 | 0 | (2,2) is expanded and has no successors |
| states 9 | `Exhausted` | 9 | 9 | 12 | 4 | 0 | the limit equals the reachable count |
| states 8 | `Bounded` | 8 | 7 | 11 | 3 | 1 | (2,2) would be the 9th state, refused while expanding (2,1) |

Water jugs: J3–J14 in [CONFORMANCE.md](CONFORMANCE.md). They include every
required boundary case: a depth limit where every successor is already seen,
a depth limit with an unseen successor, a state limit equal to the reachable
count, a state limit that excludes a reachable state, and violations that
exist only beyond a bound.

### Consequences if accepted

- **Correctness:** `Bounded` is never reported spuriously. A limit that turns
  out not to matter yields the stronger, true `Exhausted`.
- **Cost:** at most one extra BFS level of `Next` calls and key lookups (no
  insertions) under a depth limit. A state limit adds nothing.
- **Implementation:** one depth or count comparison per unseen successor, one
  new counter, and one status rule. The state limit stops inside an
  expansion, as violations already do, so SEMANTICS.md §8's sentence "limits
  are checked between state expansions" must be narrowed to time, memory, and
  cancellation.
- **Edits required on acceptance:** SEMANTICS.md §8 rules 2–3, its Grid2
  bound example, and §10; ARCHITECTURE.md §3.9; and removing the "requires
  D-012" markers from TESTING.md §7 and CONFORMANCE.md.

### Validation

CONFORMANCE.md J3–J14, the Grid2 table above, and the named tests in
TESTING.md §7. In random-graph differential tests, use limits equal to the
reference explorer's maximum depth and to its exact state count, and check
that the result is `Exhausted`.
