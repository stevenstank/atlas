# Constrained optimization (Application B)

> Status: **planned for Phase 5**. Nothing here is implemented. The design
> depends on the core engine being correct and stable first
> ([ROADMAP.md](ROADMAP.md)).

## 1. Purpose and scope

Find a configuration, within a declared finite space, that satisfies every
constraint and minimizes (or maximizes) an objective. Examples include the
cheapest replica placement that keeps a quorum per zone, the minimum-memory
cache configuration that meets a hit-rate model, or the lowest-latency choice
of batching parameters.

**Not** in scope: continuous variables, very large combinatorial problems where
a MIP/CP/SAT solver is the right tool, and metaheuristics (simulated
annealing, genetic algorithms) in the initial version.

## 2. Problem model

Atlas supports two problem shapes. Both are finite.

**(a) Assignment problems.** Variables `x₁ … xₙ`, each with a finite, ordered
domain `Dᵢ`. A configuration is a full assignment. The search tree assigns
variables in a fixed order, and values in domain order. Every partial
assignment is reached by exactly one path, so **no deduplication is needed**.

**(b) Reachable-state optimization.** Use a model-checking model
(`Init`/`Next`) and minimize the objective over the feasible states in
`Reach(M)`, for example "the reachable failure state with the fewest replicas
lost". The space is a graph, so the core visited set is required.

Common elements:

```go
// Proposed, illustrative only.
type Problem[C any] struct {
    // Feasible reports whether a complete configuration satisfies all constraints.
    Feasible func(c C) bool
    // Objective returns the value to minimize. Maximization negates it, or
    // uses Sense: Maximize.
    Objective func(c C) int64
    // Optional: Prune(partial) == true only if NO completion of partial is feasible.
    Prune func(partial C) bool
    // Optional: Bound(partial) <= Objective(c) for EVERY feasible completion c.
    Bound func(partial C) int64
}
```

**Objective type.** The proposal is `int64`, or another integer type, for the
first version. Floating-point objectives bring NaN, rounding, and
platform-dependent summation order, all of which make "the optimum" unclear.
If floats are added, NaN is reported as `ModelError`, and the documentation
states that comparisons are exact `<` on the float values returned.

**Ties.** Among configurations with equal optimal value, Atlas returns the
first one in enumeration order (variable order, then domain order, or BFS
discovery order for shape (b)). The result is therefore deterministic.

## 3. Result semantics

| Status       | Meaning | Claim |
|--------------|---------|-------|
| `Optimal`    | The search space was fully covered, either explored or pruned by a valid `Prune`/`Bound` | The returned value is the global optimum over the declared finite space. |
| `Infeasible` | Fully covered, and no feasible configuration exists | No configuration in the declared space satisfies the constraints. |
| `BestFound`  | Stopped by a limit or cancellation after finding ≥ 1 feasible configuration | Best value found so far. **Not** proven optimal. Optionally reports a proven bound and gap (§5). |
| `NoneFound`  | Stopped by a limit or cancellation before any feasible configuration was found | Nothing. Feasibility is unknown. |
| `ModelError` | Panic, NaN, nondeterminism | Nothing. |

As with model checking (SEMANTICS.md §8), only `Optimal` and `Infeasible` are
proofs. Both depend on the user's `Prune` and `Bound` functions being valid
when they are used.

## 4. Exhaustive optimization

Enumerate every configuration (shape a) or every reachable state (shape b),
evaluate `Feasible`, and keep the best feasible `Objective` value. Without
`Prune`/`Bound`, this establishes the global optimum under one assumption only:
the domain declared is the domain intended.

It costs O(∏|Dᵢ|) evaluations, so it is only practical for small spaces. Phase
5 starts here because it is easy to verify and gives the reference answer for
everything that follows.

## 5. Branch-and-bound

Depth-first search over the assignment tree. It keeps an incumbent (the best
feasible value found so far) and prunes a subtree when
`Bound(partial) ≥ incumbent` (for minimization).

**Correctness obligation (user's).** `Bound` must be a valid lower bound: it
must never exceed the objective of any feasible completion. `Prune` must only
reject partials that have no feasible completion. Atlas cannot prove either
property in general. Mitigations:

- a debug mode that, on sampled nodes of small instances, enumerates the
  completions and checks the bound, reporting `ModelError` on any violation;
- tests requiring branch-and-bound to return the **same assignment** as
  exhaustive search on every test instance.

Because ties are broken by enumeration order and DFS follows the same order,
pruning with `≥` still returns the first optimum that exhaustive search would
return. That optimum is never pruned, since its subtree bound is ≤ opt, which
is less than any incumbent found before it.

**Interrupted runs.** If stopped early, the result is `BestFound`. Atlas
also tracks the minimum `Bound` over the unexplored open subtrees. That gives
a proven lower bound `L`, and the report states "optimum lies in
[L, incumbent]". This is the only optimality information available from an
incomplete run.

## 6. Reuse of core infrastructure

See [ARCHITECTURE.md](ARCHITECTURE.md) §4 and [DECISIONS.md](DECISIONS.md)
D-009.

| Reused from `core`                         | Owned by `optimize`                    |
|--------------------------------------------|----------------------------------------|
| Generator contract (`Init`/`Next`, `AppendKey`) | Objective, constraints, `Bound`, `Prune` |
| Visited set and IDs (shape b only)         | Incumbent tracking and tie-breaking    |
| Limits, cancellation, statistics           | DFS / best-first loop                  |
| Path replay, which turns the solution into its sequence of moves | `Optimal` / `BestFound` / … result type |

`optimize` does **not** reuse `check`'s BFS loop, invariants, or `Status`
values. If `optimize` needs a stack frontier, it lives in `optimize`, unless
`check` gains a DFS that needs the same thing, in which case it moves to
`core`. No objective-related or bound-related field ever appears in `core` or
`check` types.

## 7. Heuristic and approximate search

These are not planned for Phase 5. If they are added later (for example beam
search or local search), their results are always `BestFound` or `NoneFound`,
whatever the run looks like. A heuristic run can never report `Optimal`, even
if it happened to find the true optimum.

## 8. Worked example: 0/1 knapsack

Items (weight, value): A(2,3), B(3,4), C(4,5). Capacity 5. Maximize value.

| Subset | Weight | Value | Feasible |
|--------|-------:|------:|:--------:|
| {}     | 0      | 0     | ✓ |
| {A}    | 2      | 3     | ✓ |
| {B}    | 3      | 4     | ✓ |
| {C}    | 4      | 5     | ✓ |
| {A,B}  | 5      | 7     | ✓ |
| {A,C}  | 6      | 8     | ✗ |
| {B,C}  | 7      | 9     | ✗ |
| {A,B,C}| 9      | 12    | ✗ |

Exhaustive search gives `Optimal`, {A,B}, value 7, with 5 of 8 configurations
feasible. A run limited to 3 evaluations, using domain order "exclude before
include" with variables A, B, C, sees {}, {C}, {B} and returns `BestFound`
with value 5 and no optimality claim. These numbers become Phase 5 conformance
tests.

## 9. Testing

Following [TESTING.md](TESTING.md):

- brute-force enumeration as the oracle on random small instances, comparing
  value *and* assignment;
- `Infeasible` instances, and instances where the optimum is the last
  configuration enumerated;
- interrupted runs must never return `Optimal`, checked for every
  `StopReason`;
- a deliberately invalid `Bound` must be caught by the debug check on small
  instances.

## 10. Open questions (resolve before Phase 5)

- Multi-objective problems (Pareto fronts): out of scope, or a later
  extension?
- Should shape (b) be supported in Phase 5, or should it start with shape (a)
  only? The tentative answer is (a) first.
- Best-first search (a priority queue on `Bound`) versus DFS. Best-first gives
  better bounds sooner but uses more memory. Measure before deciding.
