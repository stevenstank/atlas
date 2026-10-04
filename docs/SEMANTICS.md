# Semantics

> Status: specification for Phase 1. Nothing here is implemented yet. Where this
> document and the code disagree, one of them has a bug. Resolve the
> disagreement explicitly; do not leave it.

This document defines what an Atlas model-checking run means: what it explores,
what a verdict claims, and what it does *not* claim. The optimizer has its own
result semantics in [OPTIMIZATION.md](OPTIMIZATION.md).

## 1. Models, states, and transitions

A **model** M consists of:

- a set of **states** S (any Go value the model chooses);
- a finite, ordered list of **initial states** `Init = [s₀, s₁, …]`;
- a **successor function** `Next(s)` that returns a finite, ordered list of
  labeled steps `[(a₁, t₁), (a₂, t₂), …]`. Each `aᵢ` is an action label (for
  example `"Send(1→2)"`) and each `tᵢ` is a successor state;
- a **canonical encoding** `Key(s)`, a byte string that defines state identity
  (§3);
- an ordered list of named **invariants** `I₁ … Iₖ`, each a predicate on a
  single state.

The **transition relation** is `s → t` if and only if `(a, t) ∈ Next(s)` for
some action `a`. Nondeterminism, such as which message arrives or which process
runs, is expressed as multiple entries in `Next(s)`.

**Purity requirement.** `Init`, `Next`, `Key`, and the invariants must be pure
functions of their inputs. They must not read clocks, global mutable state,
randomness, map iteration order, or I/O, and they must not mutate their input
state. Atlas treats any state it has received as immutable. This is a
precondition. Atlas can detect some violations (§7) but not all of them.

## 2. Reachability

A state t is **reachable** if there is a finite path
`s₀ → s₁ → … → sₙ = t` where `s₀ ∈ Init`. Its **depth** is the length n of the
shortest such path. Initial states have depth 0.

Atlas explores the set `Reach(M)` of reachable states, considered **up to
identity** (§3). "Explored" means the engine computed `Next(s)` for the state.
States that are discovered but not yet expanded remain in the **frontier**.

## 3. State identity and equality

Two states are **the same state** if and only if `Key(s) == Key(t)` byte for
byte. The model is responsible for making `Key` canonical:

- **Injective on distinguishable states.** If two states can behave differently
  (have different successors or invariant results), their keys must differ.
  Otherwise Atlas merges them and may miss behaviors. This is the model
  author's obligation.
- **Equal on equivalent states.** If the model treats two representations as
  the same state (for example, a set stored as a slice in a different order),
  `Key` must normalize them. Otherwise Atlas explores duplicates. This is safe
  but wasteful.

**Hashes are not identity.** Atlas may hash keys to index its visited set. A
hash match is only a hint: the engine must confirm identity by comparing the
full keys. Two different states whose hashes collide are still stored and
explored separately. Any mode that stores only fingerprints (lossy hashing, as
TLC does) gives up this guarantee. Such a mode would be a separate, opt-in mode
whose results are labeled probabilistic. It is not part of the initial design.
See [DECISIONS.md](DECISIONS.md) D-002.

*Example.* A model that stores a set of node IDs as a `[]int` must sort the
slice inside `Key`. Otherwise `[1,2]` and `[2,1]` become two states. If the
order carries meaning (a queue, for example), it must *not* sort, because those
are different states.

## 4. Duplicate handling

When a step produces a successor whose key is already in the visited set, the
step is counted as a **transition examined** and as a **duplicate**. The
successor is not enqueued again. Its recorded parent and depth (the first
discovery) are not changed.

Because invariants are predicates on single states, checking a state once is
enough. Deduplication therefore preserves every safety verdict, provided
`Key` satisfies §3.

## 5. Invariants and violations

An invariant `I` is **violated** at state s if `I(s)` is false. If an invariant
function panics, Atlas reports a **model error**, not a violation. Recovering
from the panic and reporting it is the engine's job.

When invariants are checked:

- every state is checked **once, when it is first discovered**, before it is
  enqueued. This includes initial states;
- the invariants are evaluated in declaration order. The first failing
  invariant is the one reported for that state;
- by default the run **stops at the first violating state discovered**
  (`StopOnFirstViolation`, recommended default). A "collect all violations"
  mode is possible later. Its semantics must state which trace is reported for
  each violation.

**Deadlock.** A reachable state with an empty `Next(s)` is a **terminal
state**. Whether a terminal state counts as a violation is a configuration
option. By default it is *not* a violation, because many models have
legitimate final states. Atlas always counts terminal states in the run
statistics. This default is open in [DECISIONS.md](DECISIONS.md) D-010.

## 6. Search order and the shortest-counterexample guarantee

### BFS (initial and default algorithm)

The engine keeps a FIFO frontier. Initial states are discovered first, in
`Init` order. Then the engine repeatedly dequeues a state and processes its
successors in `Next` order.

**Guarantee.** With single-threaded BFS, invariants checked at discovery, and
the purity requirement satisfied, the first violating state found has the
**minimum depth among all reachable violating states**. The reported
counterexample therefore has the **fewest transitions** of any counterexample
for any invariant.

*Why.* FIFO BFS discovers states in order of non-decreasing depth. Each state's
recorded parent has depth one less than the state itself, so following parent
links back gives a path of exactly that depth.

**Scope of the guarantee.**

- "Shortest" means fewest steps. Every transition has cost 1. Atlas has no
  notion of time or weight on transitions. If you need the cheapest
  counterexample under weighted steps, that is a different search (a shortest
  path algorithm such as Dijkstra's) and is not provided.
- It holds only for single-threaded BFS. DFS, parallel exploration, and any
  reduction technique must state separately whether they preserve it. By
  default, assume they do not. See D-008.
- Among several shortest counterexamples, Atlas reports the one that the
  deterministic exploration order (§7) finds first. It is *a* shortest trace,
  not a unique or canonical one.
- Depth is measured in the model as given. If the model bundles several
  real-world events into one step, "shortest" refers to the bundled steps.

### Worked example: `Grid2`

States are pairs `(x, y)` with `x, y ∈ {0, 1, 2}`. There is one initial state,
`(0,0)`. `Next(x, y)` emits, in this order:

1. `IncX` → `(x+1, y)` if `x < 2`
2. `IncY` → `(x, y+1)` if `y < 2`

The invariant is `Sum: x + y < 4`.

BFS run, with `D` meaning "discovered new" and `dup` meaning "already visited":

| Expand  | Depth | IncX         | IncY         |
|---------|-------|--------------|--------------|
| (0,0)   | 0     | D (1,0)      | D (0,1)      |
| (1,0)   | 1     | D (2,0)      | D (1,1)      |
| (0,1)   | 1     | dup (1,1)    | D (0,2)      |
| (2,0)   | 2     | —            | D (2,1)      |
| (1,1)   | 2     | dup (2,1)    | D (1,2)      |
| (0,2)   | 2     | dup (1,2)    | —            |
| (2,1)   | 3     | —            | D (2,2) ✗    |

`(2,2)` violates `Sum`. Following parent links back:

```
(0,0) -IncX-> (1,0) -IncX-> (2,0) -IncY-> (2,1) -IncY-> (2,2)
```

The trace has 4 steps. No path to `(2,2)` is shorter. At the point of the
violation, Atlas has 9 distinct states, 11 transitions examined, and 3
duplicates.

If the invariant is removed, the run is **exhaustive**: 9 states, 12
transitions, 4 duplicates (the extra one is `(1,2) -IncX-> (2,2)`), 1 terminal
state (`(2,2)`), maximum depth 4. These
numbers are the first conformance test (see [TESTING.md](TESTING.md)).

*Contrast.* A DFS following `IncX` first reaches `(2,2)` through
`(1,0),(2,0),(2,1)`, which happens to be 4 steps too. On a model with a long
`IncX` chain and a short `IncY` shortcut to the bad state, DFS returns the long
path. This is why only BFS carries the guarantee.

## 7. Determinism

**Contract.** If the model satisfies the purity requirement and the run uses
the same configuration, the same Atlas version, and single-threaded BFS, then
repeated runs produce:

- the same verdict and stopping reason;
- the same counterexample (the same sequence of actions and states);
- the same values for the counted statistics (states, transitions, duplicates,
  terminal states, maximum depth).

Wall time and memory statistics are *not* covered.

The contract applies only to runs that end the same way. A run cut off by a
time limit or by cancellation stops at a point that depends on timing, so its
statistics may differ between runs.

The order comes from two sources, both under the model's control: `Init`
order and `Next` order. Atlas never iterates over a Go map, or any other
unordered structure, in a way that affects exploration order. Parallel
exploration (Phase 6) will have its own, weaker contract. See D-008.

**Detecting nondeterminism.** Atlas reconstructs counterexamples by
re-executing `Next` (see [ARCHITECTURE.md](ARCHITECTURE.md)). During replay it
checks that the recorded step produces a successor with the expected key. A
mismatch is reported as a **model nondeterminism error**, not as a
counterexample. Tests may also run a model twice and compare statistics. Atlas
cannot detect impurity that never shows up along these paths.

## 8. Bounds, termination, and completion status

Rules from [DECISIONS.md](DECISIONS.md) D-012 (accepted). Expected values for
every case are in [CONFORMANCE.md](CONFORMANCE.md).

A finite `Reach(M)` together with BFS always terminates. Atlas does not require
`Reach(M)` to be finite, but if it is infinite, the run ends only because of a
limit or an interruption.

Every run ends with exactly one **status**:

| Status        | Meaning | What it proves |
|---------------|---------|----------------|
| `Violation`   | An invariant failed in an admitted state. A trace is attached. | The model can reach a bad state. The trace is the evidence. |
| `Exhausted`   | The frontier emptied, nothing was refused, and no violation was found. | **No reachable state violates any invariant.** |
| `Bounded`     | The frontier emptied after at least one depth cutoff, or the run stopped at a state-limit refusal. No violation in any admitted state. | No violation among the admitted states, as the claim states. Nothing about excluded states. |
| `Incomplete`  | Interrupted (cancellation, deadline, memory, capacity) before any of the above. | Nothing conclusive. |
| `ModelError`  | The model panicked, broke the `emit` contract, was nondeterministic on replay, or emitted no initial states. | Nothing. |

Invalid configuration (for example N = 0 or D < 0) is rejected before `Init`
runs. It is a caller error, reported separately from these five statuses.

Rules:

1. **Only `Exhausted` is a verification result.** Reports, exit codes, and APIs
   must make it impossible to confuse `Bounded` or `Incomplete` with
   `Exhausted`. The result type offers no single `OK bool` field.
2. **Admission and check order.** Initial states have depth 0, and a
   successor of a state at depth d has depth d + 1. Each emission is handled
   in this order:
   1. already admitted → duplicate;
   2. depth > D → **cutoff**: not admitted, not checked, counted, and
      exploration continues;
   3. N states already admitted → **state-limit refusal**: not admitted, not
      checked, and the run stops with `Bounded`;
   4. otherwise it is admitted and its invariants are checked; a failure
      stops the run with `Violation`.

   `Init` emissions follow the same order; the depth check never fires for
   them. A refusal records its phase: initialization or expansion.
3. **Depth limit D (D ≥ 0)** is the maximum depth of admitted states. States
   at depth D are checked *and expanded*, so that cutoffs can be detected. If
   the frontier empties with at least one cutoff, the run is `Bounded`, and
   the claim is exact: "no violation in any state at depth ≤ D". With no
   cutoff it is `Exhausted`, even if some state lies exactly at depth D.
4. **State limit N (N ≥ 1)** counts every admitted state, including initial
   states. Admitting the N-th state does not by itself make the run
   `Bounded`. Only an unseen state that would be the (N+1)-th does. If the
   model has exactly N reachable states, the run is `Exhausted`. The claim
   covers the N admitted states, which include every state shallower than
   the refused one.
5. **Empty `Init`.** If `Init` emits nothing, the run is `ModelError` ("no
   initial states"), never a vacuous `Exhausted`.
6. **Violations.** A violation in an admitted state ends the run at once with
   `Violation`. A violating state that was cut off, refused, or never reached
   was never checked and is never reported.
7. **Interruptions** (cancellation, deadline, `MaxHeapBytes`) are observed
   only at scheduled checks:
   - once before `Init` (if it fires, `Init` never runs and every count is 0);
   - then before dequeues 1, 1+K, 1+2K, …, where K is the check interval.

   They are never observed inside `Init` or `Next`, and there is no check
   once the frontier is empty, so an empty frontier always completes
   normally. A depth cutoff is not terminal: a run interrupted after a
   cutoff is `Incomplete`, not `Bounded`. Memory checks are best-effort,
   because Go cannot hard-cap the heap, and the heap may overshoot between
   checks.
8. **Stopping a model call.** On a violation, a state-limit refusal, or a
   detected contract violation, `emit` returns `false`, and a conforming
   model returns at once (D-001). Duplicates and cutoffs return `true`.

*Example.* Run `Grid2` without its invariant, with depth limit 3. The 8
states at depth ≤ 3 are admitted, checked, and expanded. `(2,1)→(2,2)` and
`(1,2)→(2,2)` are two cutoffs, so the run is `Bounded` with "no violation at
depth ≤ 3". With depth limit 4, `(2,2)` is admitted and expanded, it has no
successors, nothing is cut off, and the run is `Exhausted` (CONFORMANCE.md
G2, G3).

## 9. What an exhaustive run proves, and what it does not

An `Exhausted` run with invariants `I₁…Iₖ` proves:

> For the model **as written**, with its configured parameters (for example
> 3 nodes and at most 2 messages in flight), every reachable state satisfies
> every invariant, assuming `Key` is injective on distinguishable states and
> the model functions are pure.

It does **not** prove:

- **anything about the production implementation.** The model is a separate
  artifact. Bugs in the gap between model and code are invisible to Atlas;
- **anything about larger parameters.** A run with 3 nodes says nothing about 4
  nodes. Small-scope results are evidence, not proof, of general correctness;
- **liveness** ("eventually X happens"), fairness, or progress properties;
- **properties of paths** (for example "X never happens twice in a row"),
  unless the model records the needed history in the state;
- **absence of deadlock**, unless the deadlock check was enabled;
- **that the invariants are the right ones.** A weak invariant passes easily.
  Tests should include deliberately broken models to confirm the invariants
  can fail ([TESTING.md](TESTING.md)).

A `Violation` proves the model can reach the bad state. Whether the real
system can is a separate question that the engineer must answer, though the
trace is usually a precise lead.

## 10. Reported statistics

Every result reports, whatever its status (`check.Stats`):

- `InitEmissions`, `InitAdmitted`, `InitDuplicates`: initial emissions
  examined, distinct initial states admitted, and repeated initial states;
- `Admitted`: all admitted states;
- `Expanded`: states whose `Next` was called, including a partial expansion
  ended by a violation or refusal;
- `Transitions`: successor emissions examined;
- `Duplicates`: transitions to an already admitted state;
- `CutoffTransitions`: transitions refused by the depth limit (each
  transition counts, even when two lead to the same state);
- `StateLimitRefusals` (0 or 1) and `RefusalPhase`;
- `TerminalStates`: expanded states whose `Next` emitted nothing;
- `MaxDepth`: greatest admitted depth, or −1 if nothing was admitted;
- `FrontierPeak`: largest frontier size;
- `WallTime`: informational, outside the determinism contract.

**Counting rule.** An emission is counted only when it is assigned to exactly
one branch: duplicate, cutoff, refusal, or admitted. An emission that fails
earlier (a panic in `AppendKey`, a detected `emit` misuse) is not counted.
Emissions after the run ends are not counted either.

**Identities.** `R_init` and `R_exp` are the refusal count for each phase.

| # | Identity | Holds on |
|---|----------|----------|
| I1 | `InitEmissions = InitAdmitted + InitDuplicates + R_init` | every run, except one with an undetected `emit` contract violation |
| I2 | `Transitions = (Admitted − InitAdmitted) + Duplicates + CutoffTransitions + R_exp` | the same runs as I1 |
| I3 | `Admitted` = number of reachable states, and `Transitions` = number of `(action, successor)` pairs over all reachable states | `Exhausted` runs only |
| I4 | `Transitions` = Σ over admitted states of their successor counts | normal completion only: `Exhausted`, or `Bounded` with `DepthLimit` |

Tests assert each identity only under its precondition
([TESTING.md](TESTING.md) §6).
