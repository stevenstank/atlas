# Prior art and positioning

> Status: research notes, gathered 2026-10-04 from each project's public README
> and API docs. **Verified** means stated in the cited source. **Assessment**
> means it is our own interpretation or a design proposal. Re-check these facts
> before relying on them, because projects change.

None of these tools is interchangeable with the others, and Atlas does not
replace any of them.

## TLA+ tools / TLC

Source: <https://github.com/tlaplus/tlaplus>,
[current-tools.md](https://github.com/tlaplus/tlaplus/blob/master/general/docs/current-tools.md)

**Verified**
- The repository contains TLC (model checker), SANY (parser), the PlusCal
  translator, and the Eclipse-based Toolbox, which the README describes as
  unmaintained. It needs Java 11+.
- TLC stores **fingerprints** of states. They are computed with one of 131
  irreducible polynomials, chosen with `-fp`. The fingerprint set can live in
  memory (`-fpmem`) or be partitioned across disk files (`-fpbits`).
- It has multiple worker threads (`-workers N|auto`), depth-first iterative
  deepening (`-dfid`), and random simulation (`-simulate`, `-depth`).
- It checks safety and liveness properties of TLA+ specifications.

**Unverified (from memory, re-check):** at the end of a run, TLC reports an
estimated probability that a fingerprint collision caused it to miss states.

**Relevant to Atlas (assessment)**
- Storing fingerprints only, without full states, saves a great deal of memory,
  but it makes exhaustiveness probabilistic. Atlas's default is exact
  identity. A fingerprint mode would be opt-in and its results labeled
  probabilistic (D-002).
- Disk-backed queues and visited sets are a proven way to scale. Atlas defers
  them (non-goal for the initial release).
- TLC sets the standard for clear reporting of distinct states, generated
  states, and queue size. Atlas's statistics follow similar definitions so
  that numbers can be compared fairly later.

**Scope difference:** TLC checks specifications written in TLA+, a
mathematical language with temporal logic. Atlas checks models written as Go
code and supports safety properties only.

## Stateright

Source: <https://github.com/stateright/stateright>,
[`Model` trait](https://docs.rs/stateright/latest/stateright/trait.Model.html),
[`CheckerBuilder`](https://docs.rs/stateright/latest/stateright/struct.CheckerBuilder.html)

**Verified**
- Stateright is a Rust library: an embedded model checker, an explorer web UI,
  and an actor runtime that runs the same actors on a real network.
- `Model` has associated types `State` and `Action`. Required methods are
  `init_states`, `actions`, and `next_state`. Provided methods include
  `properties`, `within_boundary`, and formatting and SVG helpers. The docs
  require init and action ordering to be deterministic.
- Checkers: `spawn_bfs` ("will find the shortest Path to each discovery if
  checking is single threaded"), `spawn_dfs` (less memory, no shortest path),
  `spawn_simulation`, and `spawn_on_demand`. Options include `threads`,
  `timeout`, `target_state_count`, `target_max_depth`, `symmetry`, and `serve`.
- It includes symmetry reduction and a linearizability tester.

**Relevant to Atlas (assessment)**
- This is the closest analogue: a model written in a general-purpose language,
  checked by an embedded checker. Separating `actions` from `next_state` gives
  readable action labels in traces. Atlas adopts the same idea in a single
  callback (D-001).
- Stateright's documentation states the same single-threaded caveat on
  shortest paths that Atlas's SEMANTICS.md does. Atlas states it as well.
- `within_boundary` is a model-defined bound. Atlas uses engine-level depth and
  state bounds and reports them as `Bounded`. Model-level boundaries remain
  possible as model logic.

**Scope difference:** Stateright is Rust and actor-oriented, and its models can
run as real systems. Atlas is Go and does not plan an actor runtime. A
reasonable differentiator is a Go-native engine for teams working in Go, with
explicit result-status semantics and a measured performance story. It is
not "Stateright, but faster". No such claim can be made without benchmarks
(BENCHMARKS.md §9).

## Porcupine

Source: <https://github.com/anishathalye/porcupine>

**Verified**
- Porcupine is a Go **linearizability checker** for recorded histories of
  concurrent operations. A model supplies `Init`, `Step`, an optional
  `Hash`, and description functions for visualization.
- It builds on Lowe's work and on "Faster linearizability checking via
  P-compositionality" (Horn & Kroening, 2015). It can partition histories.
- The README reports being "generally 1,000x–10,000x faster" than Knossos on
  the same test data. That is their claim and has not been checked here.
- It produces HTML visualizations of histories. Users include etcd and
  MIT 6.5840.

**Relevant to Atlas (assessment)**
- Porcupine checks *observed* histories from real executions. Atlas explores
  *possible* executions of a model. The two are complementary: an Atlas model
  could emit histories that a Porcupine-style checker validates as an
  invariant. This is a possible future direction, not planned work.
- Its `Init`/`Step` model shape is familiar to Go users, which is evidence
  that a small functional interface works well in Go.

## simtest-go

Source: <https://github.com/vlence/simtest-go>

**Verified**
- A Go library for **deterministic simulation testing**, "heavily inspired by
  TigerBeetle's VOPR". It aims to control scheduling, I/O, timers, and
  randomness. Its source has `simulator.go`, `clock.go`, `fault.go`, and
  `io.go`.
- It is early-stage: no releases at the time of reading, and very little
  documentation.

**Relevant to Atlas (assessment):** this tool runs real code under a simulated
environment with seeded randomness. It explores a sample of executions, not
all of them. Atlas does not do this. Its approach of replaying a failure from a
seed is the simulation-world counterpart of Atlas's replayable traces.

## MadSim

Source: <https://github.com/madsim-rs/madsim>

**Verified**
- A Rust deterministic simulator. It is a drop-in replacement for tokio and
  for several clients (tonic, etcd, rdkafka, S3) that runs real async code
  deterministically under a seed, with injected failures.
- It is inspired by FoundationDB's simulation testing and is used by
  RisingWave.

**Relevant to Atlas (assessment):** this tool tests the *implementation* by
sampling executions. Atlas checks a *model* exhaustively. MadSim catches bugs
in the gap between model and code, which Atlas cannot see (SEMANTICS.md §9).
Its lesson for Atlas is that determinism has to be designed in from the start.

## Summary of positioning

| Tool        | Checks          | Exploration         | Language   | Guarantee on success                 |
|-------------|-----------------|---------------------|------------|--------------------------------------|
| TLC         | TLA+ spec       | Exhaustive (+ sim)  | TLA+/Java  | Exhaustive, up to fingerprint collisions |
| Stateright  | Rust model      | Exhaustive (+ sim)  | Rust       | Exhaustive for the model             |
| Porcupine   | Recorded history| Linearization search| Go         | That one history is linearizable     |
| simtest-go  | Real Go code    | Seeded sampling     | Go         | None beyond the seeds run            |
| MadSim      | Real Rust code  | Seeded sampling     | Rust       | None beyond the seeds run            |
| **Atlas**   | Go model        | Exhaustive BFS      | Go         | Exhaustive for the model (planned)   |

**What could realistically set Atlas apart (assessment, unproven):**
1. A Go-native, embedded, exact-identity explicit-state checker with a small
   API.
2. Result semantics that cannot be misread: exhaustive vs. bounded vs.
   incomplete.
3. A published, reproducible benchmark method, applied to Atlas itself first.
4. A finite-domain optimizer that shares the same engine and reports
   proven-optimal versus best-found.

Being written in Go is not, by itself, a novelty.
