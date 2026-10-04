# Vision

> Status: specification. Nothing in this document is implemented yet.

## What Atlas is

Atlas is an **explicit-state exploration engine** written in Go. You give it a
model: a set of initial states and a function that, for any state, lists the
possible next states. Atlas then visits every reachable state one at a time,
recognizes states it has already seen, and checks properties along the way.

Two applications sit on top of this shared core:

- **Model checking (Application A, primary).** Explore all reachable states of
  a bounded model. Check that safety invariants hold in every one of them. When
  an invariant fails, produce a counterexample: a concrete, replayable sequence
  of steps from an initial state to the failing state.
- **Constrained optimization (Application B, later).** Search a finite space of
  configurations for one that satisfies the constraints and minimizes or
  maximizes an objective. Report whether the result is a proven optimum or only
  the best one found. See [OPTIMIZATION.md](OPTIMIZATION.md).

## Problems it aims to solve

Concurrent and distributed designs fail in rare interleavings that tests and
code review rarely reach. An exhaustive search of a small but faithful model
reaches every interleaving within the model's bounds. It often finds bugs at
small sizes, such as 2 or 3 nodes, a few messages, or a single crash.

Atlas targets engineers who:

- design protocols, state machines, schedulers, caches, queues, or retry logic
  and want to check them before or alongside implementation;
- prefer to write models in Go, the language they already use, rather than
  learn a separate specification language. This is a convenience trade-off,
  not a claim that Go is a better specification language;
- need reproducible failure traces they can read, replay, and turn into
  regression tests;
- later, need to find a provably optimal configuration in a finite design
  space, or at least know when they have *not* proven it.

## Why explicit-state exploration

- **It is concrete.** Every state and every step in a counterexample is an
  actual value you can print and inspect.
- **It is exhaustive within its bounds.** If a run completes, every reachable
  state was checked. Random testing cannot give that guarantee.
- **It produces minimal evidence.** Breadth-first search gives a counterexample
  with the fewest steps (see [SEMANTICS.md](SEMANTICS.md) for the exact
  conditions).
- **Its limits are clear.** The state space has to fit in finite time and
  memory. Atlas must report honestly when it does not.

## Why performance and correctness both matter

The state space usually grows exponentially with model size, so throughput and
memory decide which bounds are reachable in practice. A model checker that is
fast but wrong is worse than no checker at all, because it gives false
confidence. Atlas therefore:

1. defines its semantics first ([SEMANTICS.md](SEMANTICS.md));
2. builds a simple, correct baseline and tests it against an independent
   reference explorer;
3. measures that baseline ([BENCHMARKS.md](BENCHMARKS.md));
4. only then optimizes, and re-checks every guarantee after each optimization.

Atlas distinguishes two kinds of speed-up and measures them separately:

- **Processing states faster:** encoding, hashing, storage, allocation, and
  data layout.
- **Exploring fewer states:** for example symmetry reduction or partial-order
  reduction. These are only valid under explicit soundness assumptions.

## Model checking and optimization

Both applications enumerate states from a generator, deduplicate them, respect
resource limits, support cancellation, and report statistics. Beyond that they
differ:

| Concern            | Model checking                        | Optimization                                   |
|--------------------|---------------------------------------|-----------------------------------------------|
| Question asked     | Does any reachable state violate P?  | Which feasible configuration has the best value? |
| Natural search     | BFS (shortest traces)                 | DFS / best-first with bounds                   |
| Early exit         | First violation                       | Only when bound proves no better solution exists |
| Main result        | Verdict + trace                       | Best solution + proof status                   |

They share infrastructure, not algorithms. See
[ARCHITECTURE.md](ARCHITECTURE.md) for where the boundary lies.

## Non-goals

These are excluded from the initial release. Some may become later research
items in [ROADMAP.md](ROADMAP.md) Phase 6.

- **Verifying arbitrary production Go code.** Atlas checks the model you write.
  The model may or may not match your implementation.
- **A specification language.** Models are Go code. Atlas has no parser and no
  TLA+ dialect.
- **Replacing TLA+/TLC, Stateright, or other tools.** They have different
  scopes and long track records. See [RESEARCH.md](RESEARCH.md).
- **Liveness, fairness, and temporal logic** (LTL/CTL). Initial scope is safety
  invariants over single states. Liveness is a separate future research
  question.
- **Unbounded or infinite-state verification**, symbolic or SMT-based checking,
  and theorem proving.
- **Distributed or multi-machine exploration**, and disk-backed state storage,
  in the initial release.
- **Parallel exploration** before a correct, measured single-threaded baseline
  exists.
- **Deterministic simulation of real programs** (the domain of MadSim and
  simtest-go). The relationship may be explored later.
- **A general-purpose optimization solver.** Atlas will not compete with
  MIP/CP/SAT solvers on problems they are designed for.
- **A GUI or interactive explorer** in the initial release.

## What success looks like

At the end of the core roadmap (Phases 0–4):

- Every reference model produces exactly the expected number of reachable
  states, verdict, and (for violations) shortest counterexample length. This is
  checked against an independent naive explorer.
- Every counterexample replays through the model and reaches the reported
  violating state.
- Every result says plainly whether it is **exhaustive**, **bounded**, or
  **incomplete**, and why.
- Repeated runs of a deterministic model produce identical results and traces.
- A published, reproducible benchmark suite reports throughput, memory, and
  allocations, with profiles that explain where time goes. Any speed-up over
  the Phase 3 baseline is backed by those numbers.
- An engineer new to Atlas can write a small protocol model in Go and read the
  counterexample without help.

After Phase 5, the optimizer reports a proven optimum for small finite spaces.
When a run is stopped early, it reports the best solution found and explicitly
says it is not proven optimal.
