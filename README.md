# Atlas

Atlas is an explicit-state exploration engine written in Go. It systematically
explores every reachable state of a bounded, user-defined model, checks safety
properties, and produces reproducible counterexample traces when a property is
violated.

> **Status: Phases 1–3 and 5 complete; Phase 4 not started.**
> Phase 0 decisions are accepted ([docs/DECISIONS.md](docs/DECISIONS.md)).
> The core BFS engine (`core`, `check`) is implemented and tested against the
> conformance tables in [docs/CONFORMANCE.md](docs/CONFORMANCE.md), an
> independent reference explorer, and 1,000 random graphs, and has a
> Phase 3 performance baseline. The `optimize` package solves assignment
> problems by exhaustive search or branch-and-bound. Nothing is released yet,
> and the API may still change.

## Why

Bugs in concurrent and distributed designs hide in rare interleavings. If you
write a small, faithful model of a design, an exhaustive search reaches every
interleaving within the model's bounds. When something breaks, it hands you the
shortest sequence of steps that leads there. Atlas aims to make that workflow
available as a Go library, with results that are honest about what they prove.
See [docs/VISION.md](docs/VISION.md).

## Two applications, one core

1. **Model checking** (primary). Explore reachable states with breadth-first
   search, check invariants on every state, and report the shortest
   counterexample, or a clear statement that the run was exhaustive, bounded,
   or incomplete.
2. **Constrained optimization** (Phase 5, `optimize`). Search a finite
   configuration space for a feasible configuration that minimizes or
   maximizes an objective. Report whether the result is a *proven* optimum or
   only the best one found. See [docs/OPTIMIZATION.md](docs/OPTIMIZATION.md).

## Design principles

- **Correctness before performance.** Semantics are specified first
  ([docs/SEMANTICS.md](docs/SEMANTICS.md)), and the implementation is tested
  against an independent reference explorer.
- **Exact state identity.** States are identified by a canonical byte encoding.
  A hash match is never treated as equality.
- **Results that cannot be misread.** Only an `Exhausted` run is a
  verification result. Runs stopped by bounds, time, memory, or cancellation
  are reported as `Bounded` or `Incomplete`, never as success.
- **Deterministic and replayable.** The same model and configuration give the
  same verdict and trace. Every counterexample replays through the model.
- **Evidence-based performance.** Measure first, then optimize. Every
  performance claim will be backed by the reproducible benchmark procedure in
  [docs/BENCHMARKS.md](docs/BENCHMARKS.md). The Phase 3 baseline reports of
  the unoptimized engine, on one machine, are in
  [docs/benchmarks/](docs/benchmarks/). Atlas makes no claims of being faster
  than any other tool.
- **Standard library first.** No dependency without a measured justification.

## What Atlas checks, and what it doesn't

Atlas checks **the model you write**, not your production code. An exhaustive
run proves that every reachable state of that model, at the configured sizes,
satisfies the given invariants. It does not prove anything about larger
sizes, liveness properties, or how faithful the model is to the real system.
See [docs/SEMANTICS.md §9](docs/SEMANTICS.md#9-what-an-exhaustive-run-proves-and-what-it-does-not).

## Architecture

```
check (model checking)                 optimize (assignment problems)
  BFS · invariants · limits ·            exhaustive search · branch-and-bound ·
  cancellation · stats · traces          limits · cancellation · stats
          \
           core: model contract · canonical keys · visited set · FIFO queue
```

`optimize` does not use `check` or `core` yet: assignment problems need no
model or visited set (D-009, D-015). A model supplies its initial states, a successor function that emits
`(action, next state)` pairs, and a canonical key for each state. The engine
deduplicates states by key, records a parent link for each new state, and
rebuilds counterexamples by replaying the model. Details and alternatives:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md),
[docs/DECISIONS.md](docs/DECISIONS.md).

The model shape accepted in D-001 (option 3b). `emit` returns `false` when
the model should stop emitting:

```go
type Model[S any, A any] interface {
    Init(emit func(S) bool)
    Next(s S, emit func(A, S) bool)
    AppendKey(buf []byte, s S) []byte
}
```

## Capabilities

| Capability                                   | Status  | Phase |
|----------------------------------------------|---------|-------|
| Initial states, transitions, exact dedup     | Done    | 1 |
| BFS with shortest counterexamples            | Done    | 1 |
| Safety invariants, replayable traces         | Done    | 1 |
| Depth/state/time/memory limits, cancellation | Done    | 1 |
| Search statistics                            | Done    | 1 |
| Reference protocol models, readable traces   | Done    | 2 |
| Benchmark harness and baseline               | Done    | 3 |
| Measured performance engineering             | Not started | 4 |
| Exhaustive and branch-and-bound optimization over assignment problems | Done | 5 |
| Optimization over reachable states (shape (b)) | Deferred | — |
| Runnable examples                            | Done    | 6 |
| Symmetry / partial-order reduction, parallel BFS | Research (optional) | 6 |
| Liveness, temporal logic                     | Not planned; future research question | — |

## Roadmap

Phase 0: specification → 1: correctness-first engine → 2: protocol
models → 3: performance baseline → 4: performance engineering (not started)
→ 5: optimizer → 6: optional research and release hardening (release
packaging in progress). Phases 0–3 and 5 are complete. Each phase has
explicit exit criteria. See [docs/ROADMAP.md](docs/ROADMAP.md).

## Documentation

| Document | Contents |
|----------|----------|
| [docs/VISION.md](docs/VISION.md) | Goals, audience, non-goals, success criteria |
| [docs/SEMANTICS.md](docs/SEMANTICS.md) | What a run means and what it proves |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, data flow, alternatives |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Decision records (accepted, proposed, and open) |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Phases, dependencies, exit criteria |
| [docs/TESTING.md](docs/TESTING.md) | Testing strategy and oracles |
| [docs/CONFORMANCE.md](docs/CONFORMANCE.md) | Hand-worked expected results for conformance tests |
| [docs/BENCHMARKS.md](docs/BENCHMARKS.md) | Benchmark methodology and comparison rules |
| [docs/OPTIMIZATION.md](docs/OPTIMIZATION.md) | The optimization application |
| [docs/RESEARCH.md](docs/RESEARCH.md) | Prior art: TLC, Stateright, Porcupine, simtest-go, MadSim |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Workflow, standards, review expectations |

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md). In short: correctness first, tests
that check meaning, no unmeasured performance claims, and **no commits or
pushes on the owner's behalf**, by people or by automated tools.

## Installation and first steps

Atlas has no tagged release, and its API may still change. It needs Go 1.26
or later and has no dependencies outside the standard library. To use the
current development version:

```
go get github.com/stevenstank/atlas@main
```

Start with the runnable examples, which `go test` checks against their
printed output:

- `ExampleRun` and `ExampleRun_exhausted` in
  [check/example_test.go](check/example_test.go): a small model with a lost
  update, its shortest counterexample, and the fixed model verified.
- `ExampleSolve` and `ExampleSolve_branchAndBound` in
  [optimize/example_test.go](optimize/example_test.go): the knapsack from
  [docs/OPTIMIZATION.md](docs/OPTIMIZATION.md) §8, solved exhaustively and by
  branch-and-bound.

`go doc github.com/stevenstank/atlas/check` and
`go doc github.com/stevenstank/atlas/optimize` show the API, and
[models/](models/) holds larger reference models.
