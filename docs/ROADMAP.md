# Roadmap

> Status: plan, not a promise. Current phase: **Phase 0 (in progress)**. No
> engine code exists.

## Timeline estimate

The estimate assumes about 25 h/week. Durations are rough, and each phase ends
on its exit criteria, not on the calendar.

| Phase | Name                                   | Depends on | Est. weeks |
|-------|----------------------------------------|------------|-----------:|
| 0     | Technical investigation & specification| —          | 1.5        |
| 1     | Correctness-first exploration engine   | 0          | 3          |
| 2     | Model-checking functionality & robustness | 1       | 3          |
| 3     | Performance baseline & profiling       | 2          | 2          |
| 4     | Core performance engineering           | 3          | 4 (timeboxed) |
| 5     | Optimization application               | 1 (core API), 4 recommended | 3 |
| 6     | Advanced research & release hardening (optional) | 4, 5 | up to 8 |

The core work (Phases 0–5) totals about 16.5 weeks, roughly four months.
Phase 6 is optional and adds up to two months.

**Realism check.** Phases 0–3 are well bounded. Phase 4 is open-ended by
nature, so it is **timeboxed**. Optimizations that have not paid off by the end
of the timebox are recorded and deferred. Phase 5 could start after Phase 3 if
it uses only the baseline core, but it is sequenced after Phase 4 so that the
core API stays stable while the optimizer is built on it. Any one item in
Phase 6 (sound POR or parallel BFS in particular) can take most of the
8 weeks alone. Expect to choose one or two.

```
0 ──► 1 ──► 2 ──► 3 ──► 4 ──► 5 ──► 6 (optional)
            └──────────────────(core API)──► 5   (possible early start, not planned)
```

## Rules for every phase

- Implementation starts only after the owner explicitly authorizes the phase.
- All tests from earlier phases keep passing. A phase cannot weaken a guarantee
  that an earlier phase established.
- New behavior updates [SEMANTICS.md](SEMANTICS.md) or
  [DECISIONS.md](DECISIONS.md) in the same change.
- No commits by agents. Changes are handed to the owner in chunks of about 700
  lines or less.

---

## Phase 0: Technical investigation and specification

**Goal.** An unambiguous specification of what the engine does and proves.

**In scope.** Semantics, architecture proposal, decision records, prior-art
review, test and benchmark methodology, hand-worked examples.
**Out of scope.** Any engine code.

**Deliverables**
- [x] [VISION.md](VISION.md), [SEMANTICS.md](SEMANTICS.md),
  [ARCHITECTURE.md](ARCHITECTURE.md), [DECISIONS.md](DECISIONS.md),
  [RESEARCH.md](RESEARCH.md)
- [x] Hand-worked tiny model `Grid2` with exact expected statistics and trace
  (SEMANTICS.md §6)
- [ ] [TESTING.md](TESTING.md), [BENCHMARKS.md](BENCHMARKS.md),
  [OPTIMIZATION.md](OPTIMIZATION.md), README
- [ ] A second hand-worked model with a non-trivial shortest counterexample:
  the water-jug puzzle (3 L and 5 L jugs, bad state "big jug holds 4 L"). The
  expected shortest trace is 6 steps. Write its reachable-state count down
  before Phase 1 by enumerating by hand or with a throwaway script, kept out of
  the repository.
- [ ] Owner review: D-001–D-006 and D-011 accepted or revised

**Exit criteria**
- The state, transition, identity, and status definitions are unambiguous: two
  readers would predict the same statistics for `Grid2`.
- The documents state clearly what the checker can and cannot prove
  (SEMANTICS.md §9).
- Open questions are recorded as Unresolved in DECISIONS.md.

**Risks.** Over-specifying details that only measurement can settle. Mitigation:
mark those items as proposed and validate them in Phases 3–4.

---

## Phase 1: Correctness-first exploration engine

**Goal.** The minimal engine that implements SEMANTICS.md exactly.

**In scope.** `go.mod`; `core` (model contract, key-based visited set,
FIFO frontier, parent/edge arrays, limits, cancellation, stats, trace replay);
`check` (BFS, invariants, `Result`/`Status`); an independent reference
explorer used only in tests; reference models `Grid2`, `GridN` (parameterized
dimensions and size, with an analytic state count), and water jugs.
**Out of scope.** DFS, parallelism, CLI, custom hash tables, performance work
beyond avoiding obvious waste, the optimizer.

**Tests that must pass** (details in [TESTING.md](TESTING.md))
- `Grid2` exact statistics and trace (conformance test).
- Differential test against the reference explorer: identical reachable key
  sets and identical depth for every state, on all reference models and on
  ≥ 1,000 random finite graphs.
- Every reported counterexample replays independently and is of minimal depth.
- Every limit and status path is tested: depth bound (both the `Bounded` and
  `Exhausted` cases), state bound, timeout, cancellation, model panic,
  nondeterministic model.
- Determinism: N repeated runs produce identical results.
- `go vet`, `gofmt`, and `go test -race` are clean.

**Exit criteria**
- All reachable states are explored correctly for the reference models.
- Known safety violations are detected, with shortest traces that replay.
- The determinism contract holds in tests.
- No code path returns `Exhausted` after a limit fired (tested).

**Risks.** An interface that is awkward in practice (D-001). Mitigation: write
3 models before freezing it. Subtle canonicalization mistakes in models.
Mitigation: key-injectivity tests.

---

## Phase 2: Model-checking functionality and robustness

**Goal.** Realistic bounded protocol models and counterexamples that people
can read.

**In scope.** Models, each with a documented expected outcome:
- a concurrent register (read/write by 2–3 clients, with a linearizability-style
  invariant encoded in the state);
- a bounded message-passing protocol (for example, the alternating-bit
  protocol over a lossy, duplicating channel, which should pass);
- a task queue with retries and worker crashes (invariant: no task is lost or
  completed twice);
- a deliberately broken variant of each, with a known violation.

Also in scope: trace formatting (`String`/`Format` hooks per action and
state), a helper for writing trace-based regression tests, and a review of the
deadlock option (D-010).
**Out of scope.** Liveness, temporal logic, GUI, performance tuning.

**Tests.** Expected status and state count for every model. Broken variants
produce `Violation` with the expected invariant and trace length. A regression
test for each bug found.

**Exit criteria**
- Each model has expected outcomes recorded in its test.
- A reader can understand a deliberate bug from its counterexample alone (owner
  review).
- `Bounded` and `Incomplete` results cannot be mistaken for success. This is
  checked by reviewing every place a result is printed or returned.

**Risks.** Models too small to be interesting, or too large to finish.
Mitigation: parameterize sizes and record counts for several sizes.

---

## Phase 3: Performance baseline and profiling

**Goal.** A trusted, reproducible measurement of the unoptimized engine.

**In scope.** `internal/bench` harness, the fixed suite from
[BENCHMARKS.md](BENCHMARKS.md), environment capture, CPU, heap, and allocation
profiles, scaling curves, and a written bottleneck analysis.
**Out of scope.** Changing the engine. Fixes found here are noted and done in
Phase 4. The only exception is a correctness bug, which gets fixed with a
regression test.

**Exit criteria**
- Every benchmark checks its semantic result (state count, verdict).
- Two independent runs on the same machine agree within the noise recorded by
  `benchstat`.
- A written report names the top bottlenecks, with profile evidence. Separate
  sections cover "processing per state" and "number of states".

**Risks.** Noisy machines (WSL2, laptops). Mitigation: record the environment,
use many repetitions, and treat small differences as noise.

---

## Phase 4: Core performance engineering (timeboxed)

**Goal.** Measured improvements to the hot paths identified in Phase 3.

**Candidates**, in profile order, not decided in advance: key encoding,
hashing, visited-set layout (D-003), frontier representation, allocation
reduction, memory locality, and GC pressure.
**Out of scope.** State-space reductions (POR, symmetry) and parallelism, which
belong to Phase 6.

**Every change must**
1. pass all correctness and regression tests;
2. show before/after `benchstat` results on the fixed suite;
3. record memory vs. throughput trade-offs;
4. preserve exact identity, shortest traces, and determinism, or else be
   rejected.

**Exit criteria.** The criteria above, plus an updated baseline document. If
the timebox ends, remaining ideas are recorded with their expected benefit.

**Risks.** Optimizations that tune for the benchmarks only. Mitigation: hold
back at least one model as a validation workload.

---

## Phase 5: Optimization application

**Goal.** Exhaustive optimization over small finite configuration spaces
([OPTIMIZATION.md](OPTIMIZATION.md)).

**In scope.** The `optimize` package: problem definition (variables, domains,
constraints, objective), exhaustive search with a proven optimum, an
optimizer-specific result type, and branch-and-bound with a user-supplied
bound. Branch-and-bound comes only after exhaustive search is correct.
**Out of scope.** Heuristics and metaheuristics, continuous variables,
competing with MIP/CP solvers.

**Exit criteria**
- Constraints are checked correctly, and the objective is evaluated
  consistently (tests on hand-solved instances).
- Exhaustive runs establish the optimum over the declared domain. This is
  checked against brute-force enumeration in tests.
- Interrupted or bounded runs report `BestFound`, never `Optimal`.
- Branch-and-bound returns the same optimum value as exhaustive search on
  every test instance.

**Risks.** A user-supplied bound that is not valid silently prunes the optimum.
Mitigation: a debug mode that checks the bound on sampled nodes, and clear
documentation of the obligation.

---

## Phase 6: Advanced research and release hardening (optional)

Each item needs its own decision record, explicit soundness assumptions, and
a validation plan before any code is written. None of them is required for
the first release.

| Item                     | Soundness assumption to document                    | Validation |
|--------------------------|-----------------------------------------------------|------------|
| Symmetry reduction       | The model is symmetric under the group used, and the canonical representative is computed correctly | Reduced count equals the number of orbits; verdicts match the unreduced run |
| Partial-order reduction  | Independence and visibility relations are correct for the invariants checked; cycle proviso | Verdicts match the full run on all models; check whether traces stay shortest (usually they do not) |
| Improved state storage   | Exact identity kept (or opt-in probabilistic mode labeled as such) | Differential tests; memory measurements |
| Parallel BFS             | D-008 contract                                      | Counts equal the sequential run; `-race`; trace-length checks |
| Larger benchmark models  | —                                                    | Expected counts from an independent source |
| Release packaging        | —                                                    | Docs reviewed, examples compile, versioned API |
