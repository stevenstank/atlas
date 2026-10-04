# Benchmark methodology

> Status: methodology only. **No benchmark results exist.** The harness is
> built in Phase 3 ([ROADMAP.md](ROADMAP.md)). Every number published for Atlas
> must come from the procedure below.

## 1. Principles

1. **Correct before fast.** Every benchmark run checks its semantic result. A
   wrong answer fails the run, however fast it was.
2. **Reproducible.** The environment, command, and raw output are recorded for
   every published number.
3. **Statistical.** Repeated runs, medians, and `benchstat` comparisons. A
   single run proves nothing.
4. **Two separate questions.** "How fast is each state processed?" and "how
   many states must be processed?" are measured and reported separately.
5. **No universal targets.** Throughput depends on state size, transition
   cost, branching factor, and the memory available. A figure like "N states
   per second", given without its model, means nothing.

## 2. Metrics

| Metric                      | Definition | Source |
|-----------------------------|------------|--------|
| States discovered           | SEMANTICS.md §10 | engine stats |
| Transitions examined        | SEMANTICS.md §10 | engine stats |
| States/s                    | states discovered ÷ exploration wall time | derived |
| Transitions/s               | transitions ÷ exploration wall time | derived |
| Total exploration time      | wall time of `Run`, start to result, for `Exhausted` runs | `time` |
| Time to first counterexample| wall time until `Violation` is returned, including trace reconstruction | `time` |
| Peak heap                   | max sampled `/memory/classes/heap/objects:bytes` | `runtime/metrics` |
| Peak RSS                    | max resident set size of the process | `/usr/bin/time -v` (Linux) |
| Bytes/state                 | peak heap ÷ states discovered | derived |
| Allocations, bytes allocated| per run | `-benchmem` / `runtime.MemStats` |
| GC cycles, GC CPU fraction  | per run | `runtime/metrics` |

**Total exploration time** and **time to first counterexample** are never
combined or averaged together. They measure different behavior: one measures
throughput over the whole space, the other measures how directly the search
reaches a bug.

## 3. Fixed benchmark suite

The suite is versioned. Changing a model or its parameters creates a new suite
version, and results are compared only within the same version.

| ID  | Model                         | Size parameters         | Expected result (checked)            | Measures |
|-----|-------------------------------|-------------------------|--------------------------------------|----------|
| B1  | `Grid2`                       | fixed                   | `Exhausted`, 9 states, 12 transitions| fixed overhead |
| B2  | `GridN`                       | D=4, K ∈ {10,20,40}     | `Exhausted`, K^D states              | per-state cost, tiny states, scaling |
| B3  | 8-puzzle                      | fixed                   | `Exhausted`, 181,440 states          | moderate branching, small keys |
| B4  | Water jugs                    | 3 L / 5 L               | `Violation`, trace length 6          | time to first counterexample (micro) |
| B5  | Two-phase commit              | N RMs ∈ {3,4,5,6}       | `Exhausted`, count from Phase 2 record | protocol-shaped workload |
| B6  | Message passing (bounded, lossy) | channel cap ∈ {2,3,4} | `Exhausted`, count from Phase 2 record | larger, variable-size states |
| B7  | Task queue, broken variant    | workers/tasks scaled    | `Violation`, trace length from Phase 2 record | deep counterexample in a large space |
| B8  | `GridN` + padded payload      | payload ∈ {0,64,256} B  | `Exhausted`, K^D states              | effect of state size alone |
| V1  | Held-out validation model     | chosen in Phase 3       | recorded                             | guard against tuning only for the suite |

Expected counts for B5–B7 come from the reference explorer
([TESTING.md](TESTING.md) §1) and are recorded before any performance work.
V1 is not used to guide optimization. It is measured only to confirm that a
gain is real.

## 4. Correctness checks

Each benchmark function, and the one-shot driver:

- asserts `Status`, `StatesDiscovered`, and (for violations) the trace length
  and invariant name against the table above;
- replays the counterexample for B4 and B7;
- fails the benchmark (`b.Fatal`) on any mismatch.

## 5. Procedure

**Environment (recorded with every result set).** CPU model and core count,
RAM, OS and kernel version, whether running under virtualization or WSL2,
`go version`, `go env GOOS GOARCH GOAMD64`, build flags, `GOMAXPROCS`,
`GOGC`, `GOMEMLIMIT`, the Atlas commit hash and whether the tree had
uncommitted changes, and the suite version.

**Machine hygiene.** Plug in power, close other heavy processes, and run
nothing else during measurement. Note it when this is not possible, as on WSL2
or a shared machine. Results from different machines are never compared
directly.

**Throughput and allocations** (`testing.B`):

```
go test -run '^$' -bench . -benchmem -count 10 ./internal/bench > new.txt
benchstat old.txt new.txt
```

Report the median and the confidence interval from `benchstat`. Changes within
the noise are reported as "no significant difference".

**Peak memory.** `testing.B` reuses one process, so heap peaks get mixed
across iterations. Peak heap and RSS are therefore measured with a one-shot
driver (proposed: `internal/bench/cmd/atlasbench`). It runs one model once per
process, under `/usr/bin/time -v`, and repeats at least 5 times. Report the
median.

**Time to first counterexample.** Also measured with the one-shot driver, at
least 10 runs, median, so that one-time costs are included as a user would see
them.

## 6. Profiling

```
go test -run '^$' -bench 'B3' -count 1 -cpuprofile cpu.out -memprofile mem.out ./internal/bench
go tool pprof -top cpu.out
go tool pprof -sample_index=alloc_space -top mem.out
GODEBUG=gctrace=1 <driver> B2 ...      # GC frequency and pause behavior
go test ... -trace trace.out && go tool trace trace.out   # scheduling/GC timeline
```

The Phase 3 bottleneck report lists the top functions by CPU and by allocated
bytes for B2, B3, B5, and B6, and the share of CPU time spent in GC. Profile
files are kept with the results set. Since `*.out` files are git-ignored, the
owner decides what gets archived.

## 7. Scaling

For B2 (K = 10…40), B5 (N = 3…6), and B6 (cap = 2…4), plot states/s and
bytes/state against states discovered. The analysis looks for:

- states/s falling as the visited set grows (cache misses, GC scanning);
- bytes/state rising (data-structure overhead);
- the largest size that completes on the reference machine within a stated
  memory limit.

## 8. Reporting format

Each published result set is one file, `docs/benchmarks/<date>-<machine>.md`,
containing the environment block, the suite version, the exact commands, raw
`benchstat` output, the driver output for memory and time to counterexample,
and a short interpretation. Interpretations must not claim more than the data
supports.

**Two kinds of improvement are reported separately:**

| Kind | Example | Reported as |
|------|---------|-------------|
| Faster per state | better hashing, fewer allocations | states/s, ns/transition, B/state on the *same* state count |
| Fewer states | symmetry, POR (Phase 6) | states discovered vs. unreduced run, verdict equality, and then wall time |

A reduction that changes the verdict on any suite model is a bug.

## 9. Comparisons with other tools

Atlas makes no claims of being faster than other tools. A comparison may be
published only if **all** of these hold:

1. **Equivalent models.** The same state space, demonstrated by identical
   distinct-state counts (and verdicts) in both tools for every size compared.
2. **Equivalent guarantees.** The configurations being compared give the same
   guarantee. For example, exact storage versus a fingerprint set is not a
   like-for-like comparison unless this is stated and both sides are shown.
3. **Equivalent settings.** The same thread count (single-threaded first), the
   same memory limits, warmed-up JVMs where relevant, and the same machine.
4. **Published method.** The model sources, tool versions, flags, and raw
   output are published so others can rerun them.
5. **Honest scope.** Results apply only to the models measured, and the
   interpretation says so.

Natural candidates: TLC on TLA+ ports of the B2, B3, and B5 models, and
Stateright on Rust ports. Neither has been done.

## 10. Why there is no headline target

Exploration cost per state is roughly (key encoding + hash + visited lookup +
successor generation) per transition, plus memory per state that limits how
many states fit. Each of these varies widely between models. Atlas sets
**relative** goals instead, such as "Phase 4 reduces B/state on B3 against the
Phase 3 baseline without changing any verdict". Those goals are set only after
the Phase 3 baseline exists.
