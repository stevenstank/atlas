# Decision records

Lightweight ADRs. Each record has a status:

- **Proposed:** a recommendation exists, but the owner has not accepted it.
- **Accepted:** agreed, and code may depend on it.
- **Unresolved:** more evidence is needed before deciding.
- **Superseded:** replaced by a later record (linked).

All records below are **Proposed** or **Unresolved** as of 2026-10-04. None is
accepted yet. Phase 0 exits when D-001 through D-006 and D-011 are accepted
([ROADMAP.md](ROADMAP.md)).

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

## D-011 Module path and package layout — Unresolved

**Context.** No `go.mod` exists. The module path depends on where the
repository is hosted, which is the owner's decision.

**Recommendation.** One module. Packages: `core`, `check`, `models`,
`internal/bench`, and later `optimize`. No `cmd/` until a CLI is justified.

**Validation.** Owner confirms the module path before Phase 1 starts.
