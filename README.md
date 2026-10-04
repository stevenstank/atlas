# Atlas

Atlas is an explicit-state exploration engine written in Go. It systematically
explores every reachable state of a bounded, user-defined model, checks safety
properties, and produces reproducible counterexample traces when a property is
violated.

> **Status: specification stage (Phase 0).** There is no engine code yet. This
> repository currently contains design documents only. Nothing below
> describes working functionality unless it is explicitly marked as
> implemented, and nothing is marked that way yet.

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
2. **Constrained optimization** (planned, Phase 5). Search a finite
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
  [docs/BENCHMARKS.md](docs/BENCHMARKS.md). **There are no benchmark results
  yet**, and Atlas makes no claims of being faster than any other tool.
- **Standard library first.** No dependency without a measured justification.

## What Atlas checks, and what it doesn't

Atlas checks **the model you write**, not your production code. An exhaustive
run proves that every reachable state of that model, at the configured sizes,
satisfies the given invariants. It does not prove anything about larger
sizes, liveness properties, or how faithful the model is to the real system.
See [docs/SEMANTICS.md §9](docs/SEMANTICS.md#9-what-an-exhaustive-run-proves-and-what-it-does-not).

## Architecture (proposed)

```
check (model checking)      optimize (Phase 5)
          \                    /
           core: model contract · canonical keys · visited set
                 FIFO frontier · limits & cancellation · stats · trace replay
```

A model supplies its initial states, a successor function that emits
`(action, next state)` pairs, and a canonical key for each state. The engine
deduplicates states by key, records a parent link for each new state, and
rebuilds counterexamples by replaying the model. Details and alternatives:
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md),
[docs/DECISIONS.md](docs/DECISIONS.md).

A *proposed* model shape, not an existing API, may change:

```go
type Model[S any, A any] interface {
    Init(emit func(S))
    Next(s S, emit func(A, S))
    AppendKey(buf []byte, s S) []byte
}
```

## Planned capabilities

| Capability                                   | Status  | Phase |
|----------------------------------------------|---------|-------|
| Initial states, transitions, exact dedup     | Planned | 1 |
| BFS with shortest counterexamples            | Planned | 1 |
| Safety invariants, replayable traces         | Planned | 1 |
| Depth/state/time/memory limits, cancellation | Planned | 1 |
| Search statistics                            | Planned | 1 |
| Reference protocol models                    | Planned | 2 |
| Benchmark harness and baseline               | Planned | 3 |
| Measured performance engineering             | Planned | 4 |
| Exhaustive and branch-and-bound optimization | Planned | 5 |
| Symmetry / partial-order reduction, parallel BFS | Research (optional) | 6 |
| Liveness, temporal logic                     | Not planned; future research question | — |

## Roadmap

Phase 0: specification (current) → 1: correctness-first engine → 2: protocol
models → 3: performance baseline → 4: performance engineering → 5: optimizer →
6: optional research. The plan is about four months of core work, plus up to
two months of optional work. Each phase has explicit exit criteria. See
[docs/ROADMAP.md](docs/ROADMAP.md).

## Documentation

| Document | Contents |
|----------|----------|
| [docs/VISION.md](docs/VISION.md) | Goals, audience, non-goals, success criteria |
| [docs/SEMANTICS.md](docs/SEMANTICS.md) | What a run means and what it proves |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, data flow, alternatives |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Decision records (open and proposed) |
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

## Installation

None yet. There is nothing to install until Phase 1 is complete.
