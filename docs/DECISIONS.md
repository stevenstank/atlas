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
| D-012 | Depth limit D = maximum admitted depth. Depth-D states are checked and expanded; unseen successors at D+1 are refused as `CutoffTransitions`. State limit N includes the initial state; only an unseen in-depth state that would exceed N ends the run (`StateLimitRefusals = 1`). Check order: duplicate → depth → state → admit and check. `Bounded` only after normal completion with ≥ 1 cutoff, or at a state-limit refusal; interruption after a cutoff ⇒ `Incomplete`; `Violation` only for admitted, checked states. One limit reason per run is derived under FIFO BFS, not assumed. | `Bounded` is never spurious; limits that don't matter yield `Exhausted`; deterministic with both limits; fixes a rule that cannot be implemented. | Don't expand depth-D states (always `Bounded`); admit D+1 without expanding; end at the N-th state; state check before depth check. | One extra BFS level of `Next` calls under a depth limit. Two new statistics. SEMANTICS/ARCHITECTURE/TESTING edits on acceptance. |

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

- the SEMANTICS.md §8 Grid2 example (bound 4 ⇒ `Exhausted`) assumes depth-D
  states *are* expanded;
- ARCHITECTURE.md §3.9 said "don't expand states at depth = D";
- rule 3 (state bound N) does not say what happens when the model has exactly
  N reachable states.

### Guiding principle

**`Bounded` must be backed by evidence.** A run is `Bounded` only if the
engine generated at least one transition to a reachable, previously unseen
state and refused to admit that state because of a configured limit. Reaching
a limit is not, by itself, evidence that anything was omitted.

### Options considered

| | Depth-D states | Unseen successors beyond D | Limit equal to the true size | Cost |
|---|---|---|---|---|
| (a) | checked, not expanded | never generated | `Bounded` (cannot know otherwise) | none |
| **(b)** | **checked and expanded** | **refused; counted as `CutoffTransitions`** | **`Exhausted`** | one extra level of `Next` calls and lookups |
| (c) | checked and expanded | admitted and checked, not expanded | `Exhausted` | the claim becomes "≤ D, plus some of D+1", which is confusing |

State limit: (i) end the run as soon as the N-th state is admitted, or **(ii)
end it only when an unseen state would exceed N.**

Proposed: **(b) + (ii)**, specified below.

### Proposed rules (not yet applied to SEMANTICS.md)

**Terms.** *Admitted* means inserted into the visited set (counted in
`StatesDiscovered`). Every admitted state is checked against the invariants
immediately. The initial state is at depth 0, and a successor of a state at
depth d is at depth d + 1.

**Depth limit D (D ≥ 0)** is the maximum depth of states the engine may admit
and check.

1. States at depth ≤ D are admitted, checked, and expanded. This includes
   states at depth D.
2. A successor that is already admitted is a duplicate, handled as usual.
3. A previously unseen successor of a depth-D state lies at depth D + 1. It is
   not admitted, not checked, and not enqueued, and the transition counts as
   one `CutoffTransition`.
4. All permitted expansions, including every depth-D state, are performed
   unless a violation, a model error, a state-limit refusal, or an
   `Incomplete` condition ends the run first.
5. If the run ends normally (the frontier is empty), the result is `Bounded`
   if `CutoffTransitions ≥ 1`, and `Exhausted` otherwise.

`CutoffTransitions` counts **transitions**, not distinct omitted states. Two
depth-D states with the same unseen successor contribute 2 (Grid2 G2).
Duplicates of an *admitted* state are never cutoffs. An omitted state is
never admitted, so later transitions to it are cutoffs again, not duplicates.

**State limit N (N ≥ 1)** is the maximum number of admitted states, including
the initial state.

1. The engine never admits more than N states.
2. Transitions to already admitted states are duplicates, handled as usual.
3. If a transition reaches a previously unseen state, within the depth limit,
   while N states are already admitted, that state is not admitted or checked.
   `StateLimitRefusals` becomes 1, and the run ends immediately with
   `Bounded`.
4. Admitting the N-th state does **not** by itself imply `Bounded`.
   Exploration of the admitted frontier continues.
5. If the frontier empties without another unseen state being reached, the
   result is `Exhausted`. This includes the case where the reachable count is
   exactly N.

Initial states are subject to the same rule. If `Init` emits more than N
distinct states, the (N+1)-th one triggers rule 3.

**Check order for each successor (both limits may be set).** This order is
the normative rule. Each examined successor takes exactly one branch:

```
1. already admitted?                        → Duplicate
2. else: depth(successor) > D?              → refuse; CutoffTransitions += 1; continue
3. else: admitted count == N?               → refuse; StateLimitRefusals = 1; stop with Bounded
4. else: admit; check invariants            → violation? stop with Violation
5. enqueue
```

*Why depth comes before the state limit.* A successor beyond D could never be
admitted, whatever the value of N. Checking depth first means the state limit
only refuses states the depth limit would have admitted, so the limit reason
and counters describe what actually constrained the run. Under BFS, by the
time any successor exceeds D, no unseen in-bound state remains (see below).
So checking the state limit first would not lose any admissions. It would
lose three things: the remaining depth-D expansions (and their cutoff
counts), the `DepthLimit` reason, and the exact claim "no violation at depth
≤ D", which would be replaced by the weaker state-limit claim. The order is
fixed, and emission order is deterministic (D-006), so combined-limit runs
are reproducible.

*Both conditions can hold on the same transition.* In CONFORMANCE.md J15
(D = 6, N = 14), the transition `(4,3) -EmptySmall-> (4,0)` reaches an unseen
state at depth 7 while exactly 14 states are admitted. The check order, not
any invariant of the search, decides that it is a depth cutoff.

*At most one limit reason per run: a derived property, not a rule.* It is
guaranteed when all of the following hold. These are sufficient conditions.
The derivation below uses all four, but the conclusion would survive the
opposite check order, because the admitted count cannot change during depth-D
expansion.

1. **Single-threaded FIFO BFS.** All states at depth ≤ D−1 are expanded
   before any depth-D state.
2. **Unit-step depth.** A state's depth is its parent's depth + 1, fixed at
   first admission. With FIFO this equals the shortest-path length. Atlas
   has no transition costs (SEMANTICS.md §6).
3. **The state-limit refusal is terminal.** The run stops at the first
   refusal.
4. **The check order above.**

Under these assumptions, a state-limit refusal needs an unseen state at depth
≤ D. Such states arise only while expanding states at depth ≤ D−1, and all of
those expansions come before the first depth-D expansion, where the only
cutoffs can occur. Once depth-D expansion begins, every state at depth ≤ D is
admitted, so nothing further is admitted: each successor is a duplicate or a
cutoff. Hence a refusal, if any, comes before every cutoff and ends the run,
and the two counters are never both non-zero.

DFS, parallel exploration (D-008), or any non-FIFO order breaks assumption 1,
and both counters could then be non-zero. For any such future mode, this rule
applies: **the reported reason is the condition that ended the run**. That is
`StateLimit` if a refusal occurred, because it is terminal, and `DepthLimit`
otherwise. Both counters are always reported.

**Statistics.** Add `CutoffTransitions` and `StateLimitRefusals` (0 or 1) to
SEMANTICS.md §10 when this is accepted. The identity becomes:

`transitions = (admitted − |distinct init|) + duplicates + cutoffTransitions + stateLimitRefusals`

### Run outcomes under this proposal

Every run ends with exactly one outcome. The question is how the run ended,
not only what was observed along the way.

| Outcome | Meaning | Proves |
|---------|---------|--------|
| `Exhausted` | Exploration **ended normally** (the frontier emptied) with `CutoffTransitions = 0`, `StateLimitRefusals = 0`, and no violation. | Every reachable state was admitted and satisfies every invariant. |
| `Bounded` | Either (a) exploration **ended normally** with `CutoffTransitions ≥ 1`, or (b) it ended at a **state-limit refusal**, with no violation in any admitted state. Reason: `DepthLimit` for (a), `StateLimit` for (b). | At least one reachable state was excluded by a configured limit. No admitted state violates an invariant. Nothing about excluded states. |
| `Incomplete` | Exploration was **interrupted** before normal completion and before any terminal condition (state-limit refusal, violation, model error): cancellation, timeout, resource exhaustion, or another interruption. | Nothing conclusive. No claim that excluded states exist, nor that none do. |
| `Violation` | An invariant failed in an **admitted and checked** state, before any other terminal condition ended the run. | The model can reach a violating state. The trace is the evidence. |
| `ModelError` | The model panicked or broke the model contract, for example nondeterminism detected on replay, or mutation of an emitted state. | Nothing. |

**Precedence.** The first terminal condition reached decides the outcome:

- **Normal completion** happens only when the frontier empties. The result is
  then `Exhausted` or `Bounded` (a), depending on `CutoffTransitions`.
- **A depth cutoff is never terminal.** If a cutoff has occurred and the run
  is then interrupted before normal completion, the result is `Incomplete`,
  not `Bounded`. The counted cutoffs are reported in the statistics, but the
  run did not finish checking the admitted frontier, so it cannot support the
  `DepthLimit` claim "no violation at depth ≤ D".
- **A state-limit refusal is terminal** and returns `Bounded` (b)
  immediately. Interruptions are checked only between expansions, so none can
  intervene once it has happened.
- **A violation** returns `Violation` immediately. The engine does not keep
  exploring to find out whether the run would also have been `Bounded`.
  (Under FIFO BFS, no violation can follow a depth cutoff, because nothing is
  admitted after depth-D expansion begins. The rule is stated for
  completeness and for future search orders.)
- **A model error** returns `ModelError`. If it is detected while building a
  trace after a violation, it replaces `Violation`.
- **Interruptions** are checked before the next state is dequeued. If the
  frontier is already empty at that point, the run has completed normally.

**A proven violation and an inconclusive run are different results.**
`Violation` is reported only for a state the engine admitted and checked. A
violating state that was refused (cut off or over the state limit), or never
reached because of an interruption, was never checked and is **never reported
as found**. Such runs return `Bounded` or `Incomplete` (CONFORMANCE.md J8,
J13, J18).

**Why `Bounded` does not prove the absence of violations.** `Bounded` means
the invariants hold in every admitted state. The omitted states, and every
state reachable only through them, were never generated or checked. A
violation may exist there. J8 is a concrete example: with D = 5, the run
returns `Bounded`, yet the violating state (4,3) is one step beyond the
limit. A `Bounded` result supports only its stated claim:

- depth limit: "no violation in any state at depth ≤ D". This is exact,
  because BFS admits every state at depth ≤ D;
- state limit: "no violation among the N admitted states", which are all
  states at depth < d plus some at depth d, with d reported.

### Why (b) is sound

When the first depth-D state is expanded, every state at depth ≤ D is already
admitted, because BFS admits states level by level and no state limit
intervened. A successor that is not admitted therefore has true BFS depth
exactly D + 1. Each cutoff transition points at a real, reachable, omitted
state, which is the evidence `Bounded` requires. If the frontier empties with
no cutoff and no refusal, every successor of every admitted state was
admitted. The admitted set is then closed under `Next`, so it equals the
whole reachable set, and `Exhausted` is correct.

### Boundary cases

Every case, with model, limits, outcome, admitted count, and cutoff count, is
in [CONFORMANCE.md](CONFORMANCE.md): water jugs J3–J20 and Grid2 G2–G6. All
were verified with a throwaway script that implements the check order above.

### Consequences if accepted

- **Correctness:** `Bounded` is never reported spuriously. A limit that turns
  out not to matter yields the true and stronger `Exhausted`.
- **Cost:** at most one extra BFS level of `Next` calls and visited-set
  lookups under a depth limit, with no insertions. A state limit adds one
  comparison per unseen successor.
- **Implementation:** the five-step check order, two counters, and one status
  rule. The state limit stops inside an expansion, as violations already do,
  so SEMANTICS.md §8's sentence "limits are checked between state expansions"
  must be narrowed to time, memory, and cancellation.
- **Edits required on acceptance:** SEMANTICS.md §8 rules 2–3, its status
  table row for `Bounded`, its Grid2 bound example, and §10;
  ARCHITECTURE.md §3.9; and removing the "Proposed D-012" markers from
  TESTING.md §7 and CONFORMANCE.md.

### Validation

The CONFORMANCE.md bound tables and the named tests in TESTING.md §7. In
random-graph differential tests, use limits equal to the reference
explorer's maximum depth and to its exact state count (expect `Exhausted`),
and one less than each (expect `Bounded`).
