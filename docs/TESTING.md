# Testing strategy

> Status: plan. No tests exist yet. Applies from Phase 1 onward.

A model checker that is wrong does harm, because people trust its "no
violations". Tests here validate **meaning**: reachable sets, depths, verdicts,
traces, and completion status. Coverage percentage is a side effect, not a
goal.

All tests use the standard library (`testing`, native fuzzing). Any additional
dependency needs a justification ([CONTRIBUTING.md](../CONTRIBUTING.md)).

## 1. Test oracles

Every semantic test compares against one of these independent oracles:

1. **Hand-worked expectations**: `Grid2` (SEMANTICS.md §6) and water jugs,
   both collected in [CONFORMANCE.md](CONFORMANCE.md).
2. **Analytic counts**: `GridN` with D dimensions of size K has `K^D` states.
   The 8-puzzle has 181,440 reachable states (9!/2).
3. **The reference explorer**: a deliberately naive implementation in a
   test-only package. It shares **no code** with `core` or `check`. It stores
   every state in a `map[string]int` (key → BFS depth) keyed by `AppendKey` output, computes
   BFS depths level by level with plain slices, and returns the full set of
   reachable keys with their depths. It is kept simple enough to check by
   reading. Speed does not matter.
4. **Brute-force enumeration** for the optimizer (Phase 5).

## 2. Unit tests

Table-driven tests for each component in isolation: frontier FIFO order and
growth, visited-set insert/lookup, parent/edge recording, key helper encoders,
stats arithmetic, limit checks, and `Status` / `StopReason` formatting.

## 3. State identity and deduplication

- **Dedup correctness:** for every reference model, the set of keys discovered
  equals the set from the reference explorer, not just the count.
- **Key injectivity (model obligation):** for each reference model, a
  property test generates pairs of reachable states. If two states have equal
  keys, their successor key sets and invariant results must also be equal.
- **Canonicalization:** tests show that equivalent representations encode
  equally, for example sets stored in different orders, and that meaningful
  orderings (queues) do not.
- **Hash collisions:** the baseline Go map compares full keys, so collisions
  are handled inside the map. Any custom table (Phase 4) must offer an
  internal test hook that replaces the hash function with a degenerate one
  (constant, then 2-bit). The full differential suite must pass under that
  hook with identical results.
- **Aliasing detector:** an optional debug mode, used throughout tests. It
  re-encodes each state when it is expanded and compares the result to the key
  stored at discovery. A mismatch means someone mutated an emitted state and
  is reported as `ModelError`. A deliberately aliasing test model confirms
  that the detector fires.

## 4. Invariant violations

- Deliberately broken models must produce `Violation` with the expected
  invariant name.
- When several invariants fail in the same state, the first one in
  declaration order is reported.
- A violation in an initial state produces a trace of length 0.
- A panic in an invariant or in `Next` produces `ModelError` that identifies
  the state, never `Violation` or `Exhausted`.
- Every model that passes in the suite has a broken twin, to show its
  invariants can fail at all (SEMANTICS.md §9).

## 5. Counterexample correctness and shortest-trace guarantee

For **every** `Violation` produced by **any** test:

1. **Replay independently.** A test helper, not the engine's own replay, calls
   `Init` and then, for each step, checks that `(action, state)` appears in
   `Next(previous)` and that the final state violates the reported invariant.
2. **Check minimality.** The trace length equals the minimum depth of any
   violating state according to the reference explorer.
3. **Check determinism.** A repeated run returns the identical trace.

Water jugs: trace length exactly 6. `Grid2`: the exact trace in SEMANTICS.md.

## 6. Determinism and replay

- Run each reference model ≥ 10 times and under `GOMAXPROCS=1` and the
  default value. Verdicts, traces, and counted statistics must be identical.
- **Nondeterminism detection:** a test model whose `Next` order depends on
  map iteration, or on a counter that changes between calls, must be reported
  as `ModelError` when the engine replays a trace. It must never be reported
  as a valid counterexample.
- **Statistics identities.** Each identity has preconditions, and a test
  asserts an identity only when they hold. Counts always describe what the
  engine **examined so far**, never the full reachable graph, unless the run
  is `Exhausted`.
  - *Current semantics* (SEMANTICS.md §10, normative today):
    `transitions == discovered − |distinct init| + duplicates`. Assert it
    only on runs with **no depth or state limit configured**. Limited runs
    are not covered until D-012 is decided.
  - *D-012 semantics (accepted)* ([DECISIONS.md](DECISIONS.md) D-012,
    "Statistics and accounting identities"):
    - **I1** `InitEmissions == InitAdmitted + InitDuplicates + R_init` and
      **I2** `Transitions == (Admitted − InitAdmitted) + Duplicates +
      CutoffTransitions + R_exp` hold on every run **without an undetected
      callback-contract violation**, including `ModelError` runs. This
      follows from the D-012 counting rule: an emission is counted only when
      it is assigned to exactly one branch.
    - **I3** (Admitted and Transitions equal the reference explorer's
      reachable count and edge count) holds **only** for `Exhausted`.
    - **I4** (Transitions equals the sum of successor counts over admitted
      states) holds **only** after normal completion: `Exhausted`, or
      `Bounded` with `DepthLimit`.
    - For `Violation`, a state-limit refusal, `Incomplete`, and `ModelError`,
      tests assert I1 and I2 and the exact counts from CONFORMANCE.md, and
      must **not** assert I3 or I4.

## 7. Bounds, limits, and cancellation

### Depth and state limits (D-012 semantics)

> **D-012 (accepted 2026-10-04).** These tests follow
> [DECISIONS.md](DECISIONS.md) D-012, which supersedes SEMANTICS.md §8 rules
> 2–3.

Each named test runs every listed case from the
[CONFORMANCE.md](CONFORMANCE.md) bound tables. Each case fixes the model,
depth limit D, state limit N, invariant, expected outcome and limit reason,
and every count: Admitted, Expanded, Transitions, Duplicates,
`CutoffTransitions`, and `StateLimitRefusals`, plus the initialization counts
for M rows. The test also asserts identities I1 and I2, and asserts I3 and I4
only under their preconditions (§6).

| Test | Scenario | Expected outcome | Cases |
|------|----------|------------------|-------|
| `TestDepthLimitAllSuccessorsSeen` | Depth limit where every successor of the depth-D states is already admitted | `Exhausted`, Cut = 0 | J6, G3 |
| `TestDepthLimitUnseenSuccessor` | Depth limit with an unseen successor at D+1 | `Bounded` (depth), Cut ≥ 1 | J3, J4, J5, G2 |
| `TestDepthLimitAboveMaxDepth` | Depth limit greater than the maximum depth | `Exhausted` | J7 |
| `TestStateLimitEqualsReachable` | N exactly equal to the reachable count | `Exhausted`, SLR = 0 | J12, G4 |
| `TestStateLimitExcludesState` | N smaller than the reachable count | `Bounded` (state), SLR = 1, Admitted = N | J10, J11, G5 |
| `TestViolationBeyondBoundNotFound` | A violation exists only in omitted states | `Bounded`, **never** `Violation` | J8, J13, J18 |
| `TestViolationWithinBound` | A violation in an admitted state | `Violation`, same trace as unbounded | J9, J14 |
| `TestCombinedLimitsCheckOrder` | Both limits set; the depth check precedes the state check | as listed | J15, J16, J17, G6 |
| `TestInterruptAfterCutoffIsIncomplete` | Cancellation after a depth cutoff but before the frontier empties; and after it empties | J19 `Incomplete` (Cut = 1, never `Bounded`); J20 `Bounded` | J19, J20 |
| `TestMultipleInitialStates` | Several initial emissions with a duplicate; violation in an initial state | `Exhausted`; `Violation` with 0-step trace | M1, M2, M9 |
| `TestStateLimitDuringInit` | N reached while `Init` is still emitting | `Bounded` (state, init phase); later emissions not examined; violation among them not reported | M3, M4, M5 |
| `TestStateLimitMultiInit` | Limits with multiple initial states, refused in expansion or at depth 0 | as listed | M6, M7, M8 |
| `TestEmptyInit` | `Init` emits nothing, with or without limits | `ModelError`, every count 0, never `Exhausted` | E1, E2 |
| `TestCancelDuringInit` | Cancellation requested from inside `Init`, K = 1 | observed before dequeue 1. `Incomplete` with the counts from initialization, unless initialization already ended the run | multi-init model with no limit (expected IE 5, IA 4, ID 1) |
| `TestModelErrorCounting` | Panic in `AppendKey` on the k-th successor emission | `ModelError`; that emission is uncounted; I1 and I2 hold | any reference model with an injected panic |
| `TestCallbackContract` *(depends on D-001)* | 3b: emit after `false`, and emit on a retained callback after return. 4: yield after `false`. 3a: emissions after a terminal condition | 3b: `ModelError` (same-goroutine after `false`: guaranteed; retained callback: best effort). 4: `ModelError` via gc detection, checked on the pinned toolchain. 3a: discarded and uncounted | written once D-001 is decided |

Additional checks: under a depth limit, the admitted set equals the
reference explorer's states at depth ≤ D. Under a state limit, the admitted
set is the first N states in BFS discovery order, and the report states the
complete depth d.

### Other limits

| Scenario                                  | Expected status | Extra checks |
|-------------------------------------------|-----------------|--------------|
| Timeout on a large model                  | `Incomplete`    | returns within timeout + slack |
| Canceled context (before start, mid-run)  | `Incomplete`    | before start: every count 0 and `Init` not called; mid-run: I1 and I2 hold |
| Invalid configuration (N = 0, D < 0)      | rejected before `Init`, not `ModelError` *(D-012)* | no state examined |
| Memory limit on a large model             | `Incomplete`    | reason = MemoryLimit |
| Violation before any limit                | `Violation`     | limits do not mask it |
| Infinite model, no bounds, with timeout   | `Incomplete`    | never `Exhausted` |

A dedicated test enumerates every `StopReason` and asserts that none of them
leads to `Exhausted`. Under the D-012 proposal, a depth or state limit that
refuses no successor is not a stop reason at all: the run simply exhausts.

## 8. Property-based tests

The stdlib-only generator produces **random finite directed graphs** from a
seed: node count, out-degree distribution, initial nodes, and a random "bad"
subset. Each graph is wrapped as a model, with key = node ID. For each graph,
the engine and the reference explorer must agree on the reachable set, the
depths, the verdict, and (for violations) the trace length. Run at least 1,000
seeds in normal CI-style runs and more in long runs. The seed of any failing
case is printed so it can be reproduced.

## 9. Fuzz testing

Go native fuzzing (`go test -fuzz`), with these targets:

- the random-graph differential test, using fuzzed graph parameters;
- the key helper encoders: round-trip and injectivity on fuzzed values;
- (Phase 4) any custom hash table, compared against a Go map under fuzzed
  operation sequences.

Inputs that crash the fuzzer are committed by the owner as regression corpus
entries under `testdata/fuzz`.

## 10. Regression tests

Every bug fix comes with a test that fails before the fix and passes after it.
The test names the bug, for example `TestRegression_DepthBoundOffByOne`.
Minimized models go under `testdata/` or in the test file.

## 11. Benchmarks as tests

Every benchmark checks its expected result ([BENCHMARKS.md](BENCHMARKS.md)
§4). A benchmark that finishes with the wrong state count fails.

## 12. Checks before handing work to the owner

```
gofmt -l .            # must print nothing
go vet ./...
go test ./...
go test -race ./...   # required for concurrency changes; recommended always
```

Report exactly which of these were run and what each one output.

## 13. What the tests cannot show

The tests show that Atlas implements its semantics on the models tested. They
cannot show that a user's model is faithful to a real system, or that a
user's `AppendKey` is injective on that user's model. The key-injectivity
property test (§3) is offered as a helper that users can run on their own
models.
