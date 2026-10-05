# Decision records

Lightweight ADRs. Each record has a status:

- **Proposed:** a recommendation exists, but the owner has not accepted it.
- **Accepted:** agreed, and code may depend on it.
- **Unresolved:** more evidence is needed before deciding.
- **Superseded:** replaced by a later record (linked).

On 2026-10-04 the owner explicitly accepted D-001 (option 3b), D-002, D-003
(baseline layout), D-004, D-005, D-006, D-011, and D-012. That meets the
Phase 0 exit criterion ([ROADMAP.md](ROADMAP.md)). The other records keep the
status shown in their headings.

## Phase 0 decisions (accepted 2026-10-04)

**All rows below are Accepted.** D-007 and D-009 remain Proposed, and D-008
and D-010 remain Unresolved. None of those four blocks Phase 1, so they are
not listed.

| ID | Decision | Rationale | Alternatives considered | Key consequences |
|----|-------------------|-----------|-------------------------|------------------|
| D-001 | `Model[S, A]` with `Init`, `Next`, `AppendKey(buf, s)`, using a push-style API. **Accepted: 3b** `emit func(A, S) bool` (not chosen: 3a `emit func(A, S)`, 4 `iter.Seq2[A, S]`). Emitted states are immutable and owned by the engine. | Action labels for traces; no slice allocated per state. 3b and 4 also let the engine stop a model early and detect emissions after a stop. | `Next(s) []S`; `Actions` + `NextState` (Stateright). | 3a can never interrupt a model's `Init`/`Next` call: work is wasted, and an endlessly emitting call hangs the run. 3b and 4 rely on models honoring the stop; 4's misuse detection is gc behavior, not spec. 3b and 4 put a stop obligation on every model. Models must copy before mutating. |
| D-002 | Identity = exact canonical byte key. No fingerprint-only mode in Phases 1–4. | Exact verdicts. Hash collisions cannot cause missed states. Works for any state shape. | `S comparable` with `map[S]ID`; 64-bit fingerprints (TLC-style). | Cost of encoding every successor. The model author must make the key injective and canonical. Helper encoders are needed. |
| D-003 | Baseline: `map[string]StateID` + `parent`/`edge`/`depth` slices + FIFO of `(ID, S)`. Optimized layout deferred. | Simplest correct design, using the map's built-in collision handling. Measure before redesigning. | Arena + open addressing; packed fixed-width keys; frontier stores IDs and decodes. | Higher memory per state and GC pressure, both known up front. Phase 4 may replace it, behind the same semantics. |
| D-004 | Single-threaded BFS only in Phase 1. DFS later, only if measured to be needed. | BFS alone guarantees shortest traces. | DFS; iterative deepening; random simulation. | Wide models may exhaust memory in the frontier. |
| D-005 | Store `(parent, edge ordinal)` per state; rebuild traces by replaying `Next`. | About 8 B/state. Replay also detects nondeterminism. | Store `(parent, action)`; store full states. | Trace building costs extra `Next` calls. Requires deterministic models, which D-006 requires anyway. |
| D-006 | Pure model + same config + single-threaded BFS ⇒ identical verdict, trace, and counted stats. Order comes only from `Init`/`Next`. | Reproducible bugs and regression tests; replay depends on it. | Weaker contract (verdict only); seeded randomized order. | Constrains parallelism (D-008) and any randomized data structure. Timing-cut runs are excluded from the contract. |
| D-011 | Module `github.com/stevenstank/atlas` (**assumes** that GitHub location); `go 1.26`; packages `core`, `check`, `models`, `internal/bench`. | One module keeps things simple. The directive matches the tested toolchain. | Another host or path; a lower directive such as `go 1.23` for wider compatibility. | The path is costly to change after imports exist. The directive sets the minimum Go version for every user. |
| D-012 | Depth limit D = maximum admitted depth. Depth-D states are checked and expanded; unseen successors at D+1 are refused as `CutoffTransitions`. State limit N includes the initial state; only an unseen in-depth state that would exceed N ends the run (`StateLimitRefusals = 1`). Check order: duplicate → depth → state → admit and check. `Bounded` only after normal completion with ≥ 1 cutoff, or at a state-limit refusal; interruption after a cutoff ⇒ `Incomplete`; `Violation` only for admitted, checked states. One limit reason per run is derived under FIFO BFS, not assumed. Multiple initial states use the same check order (a refusal during `Init` ends the run). Counters count only examined emissions; identities I1–I4 have explicit preconditions. Invalid limits are a caller error, not `ModelError`. | `Bounded` is never spurious; limits that don't matter yield `Exhausted`; deterministic with both limits; fixes a rule that cannot be implemented. | Don't expand depth-D states (always `Bounded`); admit D+1 without expanding; end at the N-th state; state check before depth check. | One extra BFS level of `Next` calls under a depth limit. Two new statistics. SEMANTICS/ARCHITECTURE/TESTING edits on acceptance. |

---

## D-001 Model interface and state ownership — Accepted (2026-10-04): option 3b

**Context.** The interface determines how easy models are to write, how much
the engine allocates, and whether traces carry readable action labels.

**Options.**
1. `Next(s) []S`: simple, but allocates a slice per state and has no action
   labels.
2. `Actions(s) []A` + `NextState(s, a) S`, as in Stateright: readable labels,
   but two calls and a slice per state.
3. **Push callback.** Action labels, no slice per state. Two variants:
   - **3a** `Next(s S, emit func(A, S))`, `Init(emit func(S))`. The engine
     **cannot interrupt** a synchronous `Init` or `Next` call partway through
     its emissions. When a violation, a state-limit refusal, or a detected
     model error ends the run, the engine discards the remaining emissions,
     uncounted, until the call returns. Consequences:
     - the state limit still bounds what is *admitted*, but not how long the
       call runs;
     - cancellation is not observed until the call returns;
     - an `Init` or `Next` that emits without end never returns, so the run
       hangs whatever N, the time limit, or cancellation says;
     - a model cannot ignore a stop signal, because there is none, so there
       is nothing to detect.
   - **3b** `Next(s S, emit func(A, S) bool)`, `Init(emit func(S) bool)`. The
     contract is common to both:
     - `true` means continue. `false` means normal early termination, not an
       error. The model must make no further `emit` calls and must return
       promptly. Computing without emitting is legal but wasted.
     - `false` never means "skip this successor and continue".
     - The engine returns `false` only when the run is ending: the emission
       produced a violation, a state-limit refusal, or a detected contract
       violation. Duplicates and depth cutoffs return `true`.
     - **Validate, then count.** On each `emit` call the engine first checks
       the contract (the call is inside the active `Init`/`Next` and no
       `false` was returned yet). Only a valid emission can be counted (D-012
       "Counting rule"). An invalid one is never counted, and the run ends
       with `ModelError`.
     - **Synchronous and confined.** `emit` may be called only synchronously,
       from the goroutine running `Init`/`Next`, during that call.
       - *Guaranteed detection:* a same-goroutine call after `false` within
         that call → `ModelError`.
       - *Best effort:* the engine invalidates each callback when the call
         returns, so a later call on a retained callback is reported as
         `ModelError` if the run is still in progress and the call is
         observed.
       - *Undefined:* a call from another goroutine, or after the run has
         finished. Results of such a run carry no guarantee.
     - Earlier steps keep their positions, so replay (D-005) is unaffected.
4. **Iterator.** `Next(s S) iter.Seq2[A, S]`, `Init() iter.Seq[S]`. The engine
   consumes with `for … range` and stops by leaving the loop, which makes
   `yield` return `false`. Two kinds of guarantee apply:
   - *Language guarantee* (Go spec, range over functions, verified in the
     go1.26.1 spec text): after `false`, yield "must not be called again".
     The spec does **not** say what happens if it is.
   - *Toolchain behavior, not a spec guarantee:* gc go1.26.1 raises a
     recoverable run-time panic on a yield after `false`, and on a yield
     after the loop has exited. This was observed in a scratch program on
     2026-10-04 and not tested for concurrent calls. If the engine relies on
     it to report `ModelError`, that is gc-specific detection, re-checked
     when the `go` directive changes.

   Counting, confinement, and the reasons the engine stops match 3b, but the
   engine never sees an emission it could validate before the language does.
   It needs `go` ≥ 1.23, which is compatible with D-011's proposed `go 1.26`.
   It feels idiomatic, but each call may allocate a closure; this is
   unmeasured.

**Decision (accepted by the owner, 2026-10-04).**
**Option 3b:** `Init(emit func(S) bool)` and `Next(s S, emit func(A, S) bool)`,
with the contract given under 3b above, generic `Model[S, A any]`, and
`AppendKey(buf, s) []byte` (ARCHITECTURE.md §3.1). Emitted states are
immutable and owned by the engine. Reasons:
- **Over 3a:** the engine can ask a cooperating model to stop at a violation
  or a state-limit refusal, so wasted work is bounded. A same-goroutine
  emission after `false` is detected reliably.
- **Over 4:** misuse detection is the engine's own check, not gc runtime
  behavior that the language spec does not guarantee. There is also no open
  question about per-call closure allocation.

What 3b does **not** do: it cannot interrupt model code that never returns,
or that ignores `false` (keeps computing, or emits again from another
goroutine where detection is not guaranteed). Such code still hangs or wastes
the run, as under every option.

Bound and initialization semantics (D-012) are the same under all three options. Only
the handling of emissions after termination differs (D-012, Initialization
rule 7).

**Trade-offs.** 3a is the simplest for model authors, but it can never
interrupt a model's call (see the consequences above). 3b and 4 let a
cooperating model stop early, and they detect some contract violations, but
every model must honor the stop signal. 4's detection relies on gc behavior,
not the language spec.
**Effect on current text:** SEMANTICS.md §8 says one call to `Next` "always
completes once it has started". That is true under 3a, but under 3b or 4 a
violation or refusal ends the call early. Update that sentence when D-001 is
decided. Callbacks and iterators are less familiar
to new Go users than returned slices. Generics rule out mixing different
model types behind one interface value, which no current requirement needs.

**Validation.** Write the Phase 0 tiny model (`Grid2`) and one protocol model
in options 3a, 3b, and 4, and compare readability and allocations per
expansion with `testing.AllocsPerRun`.

## D-002 State identity and canonicalization — Accepted (2026-10-04)

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

## D-003 Visited set and frontier design — Accepted (2026-10-04) for the baseline layout; optimized layout Unresolved

**Context.** These two structures dominate memory and are likely CPU hot spots
(unmeasured).

**Options.** See ARCHITECTURE.md §3.2–3.3. Visited: a Go map, an arena with
open addressing, or packed fixed-width keys. Frontier: live `S` values, or IDs
plus decode.

**Recommendation.** Baseline: `map[string]StateID`, parallel `parent`/`edge`
slices, and a FIFO of `(ID, S)`. Defer the optimized design until Phase 3
profiles exist.

**Trade-offs.** The baseline puts GC pressure on string pointers and uses more
memory per state than custom tables. That cost is taken on for simplicity and
correctness.

**Validation.** The Phase 3 baseline measurements, and Phase 4 A/B tests on
the fixed suite with identical semantic results.

## D-004 Search algorithm selection — Accepted (2026-10-04)

**Recommendation.** Single-threaded BFS is the only algorithm in Phase 1. DFS
is added only if Phase 3 shows the frontier is the memory bottleneck on real
models. Its results must not claim shortest traces.

**Trade-offs.** BFS frontiers can be very large on wide models.

**Validation.** Shortest-trace tests against the reference explorer
(TESTING.md §5).

## D-005 Counterexample representation — Accepted (2026-10-04)

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

## D-006 Determinism guarantees — Accepted (2026-10-04)

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

**Phase 2 observation (2026-10-05, not a decision).** The concurrent-register
model ends only in legitimate terminal states (every client has finished its
operations): 9 of 34 states at 2×1, 901 of 25,543 at 3×2. Treating them as
violations would make every register run fail, so the model gives no reason
to change the default now. The alternating-bit model has exactly one
terminal state (all messages delivered and acknowledged), also legitimate.
Neither model has a deliberate deadlock, which the validation above still
needs.

## D-011 Module path, Go directive, and package layout — Accepted (2026-10-04)

**Context.** No `go.mod` exists. Two values must be fixed before Phase 1
creates it.

**Module path: `github.com/stevenstank/atlas`.**
*Approved assumption:* the repository will be published at
`https://github.com/stevenstank/atlas`. If it is hosted elsewhere, or renamed,
the path must change before any package imports it. Changing it afterward
means rewriting every import.

**Go directive: `go 1.26`.** This is a **compatibility decision**,
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

## D-012 Completion status under depth and state bounds — Accepted (2026-10-04)

### Problem

SEMANTICS.md §8 rule 2 says states at depth D are "not expanded", and also
that the run is `Exhausted` if "no state at depth D has any successor". The
engine cannot know whether a state has successors without calling `Next`, so
the rule **cannot be implemented as written**. It also contradicts other
documents:

- the SEMANTICS.md §8 Grid2 example (bound 4 ⇒ `Exhausted`) assumes depth-D
  states *are* expanded;
- ARCHITECTURE.md §3.9 said "don't expand states at depth = D" (since rewritten);
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

### Rules (accepted; SEMANTICS.md §8 and §10 still need the text folded in)

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
≤ D. Such states arise only during initialization (depth 0) or while
expanding states at depth ≤ D−1. Both come before the first depth-D
expansion, which is where the only cutoffs can occur. Once depth-D expansion begins, every state at depth ≤ D is
admitted, so nothing further is admitted: each successor is a duplicate or a
cutoff. Hence a refusal, if any, comes before every cutoff and ends the run,
and the two counters are never both non-zero.

DFS, parallel exploration (D-008), or any non-FIFO order breaks assumption 1,
and both counters could then be non-zero. For any such future mode, this rule
applies: **the reported reason is the condition that ended the run**. That is
`StateLimit` if a refusal occurred, because it is terminal, and `DepthLimit`
otherwise. Both counters are always reported.

### Initialization (multiple initial states)

Nothing in the decision records restricts a model to one initial state.
`Init` may emit any number of states, in a deterministic order, including
repeats. Proposed rules:

1. **Depth.** Every initial state has depth 0. Because D ≥ 0, the depth check
   never refuses an initial state.
2. **Same check order as successors.** For each emission the engine examines:
   already admitted → `InitDuplicates += 1`; otherwise, if N states are
   already admitted → refusal (rule 4); otherwise admit it
   (`InitAdmitted += 1`) and check its invariants immediately.
3. **Violation in an initial state.** The run stops at once with `Violation`
   and a 0-step trace. Later `Init` emissions are not examined.
4. **State limit during initialization.** N counts initial states. If `Init`
   emits a distinct, unadmitted state while N states are already admitted,
   that state is not admitted or checked, `StateLimitRefusals = 1` (phase:
   initialization), and the run ends **immediately** with `Bounded`
   (`StateLimit`). No state is expanded.
5. **Incomplete initial set.** Rules 3 and 4 end initialization early, so the
   initial set may be only partly examined. Emissions after that point are
   **not examined and not counted**. A `Bounded` claim then covers only the
   admitted initial states. A `Violation` stands as proven regardless.
6. **Interruptions.** Follow the schedule in "Interruption check schedule"
   below. `Init` is not interrupted. A cancellation requested during `Init`
   is observed at the first check after `Init` returns, unless initialization
   already ended the run (violation, refusal, empty initial set, or model
   error). This is acceptable for Phase 1 and would need revisiting for very
   large initial sets.
7. **How the model's `Init` call stops after a violation or refusal depends
   on D-001.** Under 3a, `Init` keeps running and the engine discards its
   remaining emissions uncounted. Under 3b, the engine returns `false` and
   the model must return. Under 4, the engine leaves its `range` loop. A
   later emission is handled as described in D-001 for each option. Rules
   1–6 and 8 do not depend on the choice.
8. **Empty initial set.** If `Init` returns having emitted nothing
   (`InitEmissions = 0`), the run ends with **`ModelError`** ("no initial
   states"). The model contract requires at least one initial state.
   - *Why not `Exhausted`:* it would "prove" every invariant over zero
     states, which is vacuously true and almost always a modeling or
     parameter mistake, and it would print like a verification.
   - *Why not invalid configuration:* emptiness is a property of the model's
     behavior, observed only at run time, not of the caller's limits.
   - *Alternative, not proposed:* `Exhausted` with 0 admitted states, for
     anyone who wants vacuous runs to be legal.
   - The check runs as soon as `Init` returns, before the first dequeue
     check, so it takes precedence over a cancellation requested during
     `Init`. A cancellation observed *before* `Init` gives `Incomplete`, and
     `Init` is never called. See CONFORMANCE.md E1–E2.

### Statistics and accounting identities

Proposed counters. Every one counts only what the engine **observed**:

| Counter | Counts |
|---------|--------|
| `InitEmissions` | Initial-state emissions examined |
| `InitAdmitted` | Distinct initial states admitted |
| `InitDuplicates` | Examined initial emissions whose state was already admitted |
| `Admitted` (`StatesDiscovered`) | All admitted states: initial plus newly admitted successors |
| `Transitions` | Successor emissions examined |
| `Duplicates` | Examined successor transitions to an already admitted state |
| `CutoffTransitions` | Examined successor transitions refused by the depth limit |
| `StateLimitRefusals` | 0 or 1, with its phase (initialization or expansion) |

**Counting rule.** An emission is **examined**, and counted in
`InitEmissions` or `Transitions`, at the moment the engine assigns it to
exactly one branch: duplicate, depth cutoff, state-limit refusal, or admit.
The branch counter is incremented in the same step. Nothing is counted
before that. Therefore:
- an emission that fails before a branch is assigned is **never counted**.
  Examples: a contract violation detected on receipt (D-001 3b), or a panic
  in `AppendKey` while computing its key. The run ends with `ModelError`;
- an admitted state is counted **before** its invariants run. A violation,
  or a panic inside an invariant (`ModelError`), leaves it counted as
  admitted;
- emissions the engine never received (the model stopped early) or
  discarded after a terminal condition (3a) are not examined and appear in
  no counter.

These counters are maintained only by engine code, so the rule holds on
every run, including `ModelError` runs, with one exception: a contract
violation that is **undetected** (D-001: an `emit` from another goroutine, or
after the run ended) makes that run's results undefined, counters included.

**Identities and their preconditions.** Let `R_init` and `R_exp` be 1 if the
refusal happened in that phase, else 0.

| # | Identity | Valid when |
|---|----------|------------|
| I1 | `InitEmissions = InitAdmitted + InitDuplicates + R_init` | every run without an undetected contract violation, including `ModelError` runs, by the counting rule. Runs canceled before `Init` and empty-`Init` runs hold trivially (0 = 0). |
| I2 | `Transitions = (Admitted − InitAdmitted) + Duplicates + CutoffTransitions + R_exp` | same as I1. Each counted transition took exactly one branch; the violating transition is counted as admitted; uncounted emissions are excluded on both sides. |
| I3 | `Admitted` = the number of reachable states, and `Transitions` = the number of `(action, successor)` pairs emitted by `Next` summed over all reachable states. Every emitted pair counts, even when distinct actions lead to the same successor; this is not the number of distinct destination states. | only `Exhausted` runs, where every admitted state was fully expanded and nothing was refused. |
| I4 | `Transitions` = Σ over admitted states of the number of their successors | runs that ended **normally**: `Exhausted`, or `Bounded` (`DepthLimit`). Not after a violation, a state-limit refusal, an interruption, or a model error, where the last expansion may be partial and the frontier unexpanded. |

The current normative identity (SEMANTICS.md §10) is I2 with no cutoff or
refusal terms and `|Init distinct|` for `InitAdmitted`. It holds for runs with
no depth or state limit configured.

### Invalid configuration

Invalid limits, such as N = 0, D < 0, or a negative timeout, are a **caller
error, not a model error**. `ModelError` means the model's code misbehaved.
Proposal: reject invalid configuration before `Init` is called, report it
separately from the five outcomes, and examine no state. A limit that is
*unset* means "no limit", and must be distinguishable from 0. D = 0 is valid:
it admits and expands only initial states. The error type and API shape are
deliberately left open (D-001, ARCHITECTURE.md §3.7).

### Run outcomes under this proposal

Every run ends with exactly one outcome. The question is how the run ended,
not only what was observed along the way.

| Outcome | Meaning | Proves |
|---------|---------|--------|
| `Exhausted` | Exploration **ended normally** (the frontier emptied) with `CutoffTransitions = 0`, `StateLimitRefusals = 0`, and no violation. | Every reachable state was admitted and satisfies every invariant. |
| `Bounded` | Either (a) exploration **ended normally** with `CutoffTransitions ≥ 1`, or (b) it ended at a **state-limit refusal**, with no violation in any admitted state. Reason: `DepthLimit` for (a), `StateLimit` for (b). | At least one reachable state was excluded by a configured limit. No admitted state violates an invariant. Nothing about excluded states. |
| `Incomplete` | Exploration was **interrupted** before normal completion and before any terminal condition (state-limit refusal, violation, model error): cancellation, timeout, resource exhaustion, or another interruption. | Nothing conclusive. No claim that excluded states exist, nor that none do. |
| `Violation` | An invariant failed in an **admitted and checked** state, before any other terminal condition ended the run. | The model can reach a violating state. The trace is the evidence. |
| `ModelError` | The model panicked or broke the model contract, for example nondeterminism detected on replay, mutation of an emitted state, an empty initial set (Initialization rule 8), or a detected callback-contract violation (D-001). | Nothing. |

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
- **Interruptions** are observed only at the scheduled checks (next
  subsection).

### Interruption check schedule

Cancellation, timeout, and resource limits are all observed only at these
points:

1. **C0, once before `Init` is called.** If it fires, the result is
   `Incomplete` with every count 0, and `Init` is never called.
2. **Before dequeues 1, 1+K, 1+2K, …**, where dequeues are numbered from 1
   and K ≥ 1 is the configured interval (ARCHITECTURE.md §3.9). If it fires,
   the result is `Incomplete`, with the counts observed so far.
3. **No check without a dequeue.** If the frontier is empty, no dequeue
   happens and therefore no check. The run has completed normally
   (`Exhausted` or `Bounded`), even if cancellation was requested earlier
   (CONFORMANCE.md J20).
4. **Never inside `Init` or `Next`.** A request made during either call is
   observed at the next scheduled check after the call returns, unless the
   run ends first. With K > 1, a request may wait up to K − 1 further
   expansions.

J19 and J20 use K = 1, so a check precedes every dequeue. J19's request
(after expansion 11) is observed before dequeue 12 with `(0,1)` still queued,
giving `Incomplete`. J20's request (after expansion 12) meets an empty
frontier, giving normal completion.

**A proven violation and an inconclusive run are different results.**
`Violation` is reported only for a state the engine admitted and checked. A
violating state that was refused (cut off or over the state limit), or never
reached because of an interruption, was never checked and is **never reported
as found**. Such runs return `Bounded` or `Incomplete` (CONFORMANCE.md J8,
J13, J18, M4).

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
- **Edits required on acceptance** (done: markers updated 2026-10-04; rule
  text folded into SEMANTICS.md §8 and §10 and ARCHITECTURE.md §2 and §3.9 on
  2026-10-05): SEMANTICS.md §8 rules 2–3, its status
  table row for `Bounded`, its Grid2 bound example, and §10 (new counters,
  identities I1–I4, and replacing "repeated `Init` entries are ignored" with
  `InitDuplicates`); ARCHITECTURE.md §2 (the data flow: initial duplicates
  counted, the depth and state checks, cutoffs, the empty-`Init` check) and
  §3.9 (the check schedule); and removing the pending-decision
  notes ("Pending decision", "pending D-012", "*(D-012)*") from SEMANTICS.md,
  ARCHITECTURE.md, TESTING.md §6–§7, and CONFORMANCE.md.

### Validation

The CONFORMANCE.md bound tables and the named tests in TESTING.md §7. In
random-graph differential tests, use limits equal to the reference
explorer's maximum depth and to its exact state count (expect `Exhausted`),
and one less than each (expect `Bounded`).

## D-013 Readable results and traces — Proposed (2026-10-05)

**Context.** Phase 2 needs `String`/`Format` hooks for states and actions,
and printed results that cannot be mistaken for success.

**Decision.** No new interface. `Trace`, `*Counterexample`, and `Result` gain
`String` methods. States are printed with `%+v` and actions with `%v`, so a
model type that implements `fmt.Stringer` or `fmt.Formatter` controls its own
text, and any other type falls back to fmt's default. `Result.String` starts
with the status. Only `Exhausted` says "verified". `Bounded`, `Incomplete`,
`ModelError`, and unknown statuses say "NOT VERIFIED" and the stop reason. A
`Violation` prints the counterexample. Every result ends with the state,
transition, and terminal-state counts (the D-010 mitigation).

**Alternatives.** A `Formatter[S, A]` field in `Config`: more flexible, but
it adds API that fmt already covers. Requiring `String` on every state:
rejected because Atlas must work with plain types.

**Trace-regression helper.** `internal/tracetest.Expect` is internal, not
public API: it is a test convenience, and its shape may change.
