# Phase 3 bottleneck analysis: 2026-10-06 baseline (Ryzen 7 7435HS, WSL2)

This is the written bottleneck analysis required by the Phase 3 exit criteria
([../ROADMAP.md](../ROADMAP.md)). It interprets the measurements in
[2026-10-06-ryzen7-7435hs-wsl2.md](2026-10-06-ryzen7-7435hs-wsl2.md) (the
"baseline report") and the profiles listed in its section 9. It adds no new
benchmark runs and no new profiles. **No optimization was implemented in
Phase 3**, and nothing in `check/`, `core/`, or `models/` was changed.

Evidence is labeled as follows:
**[M]** directly measured (`atlasbench` sets A and B, 20 runs per workload);
**[P]** profile observation (`go tool pprof` on the archived files);
**[D]** arithmetic derived from [M] or [P];
**[I]** interpretation; **[H]** hypothesis for Phase 4, not established.

## 1. Executive summary

- **Per-state work is dominated by the visited set** in the two large
  exhaustive workloads that were profiled. In B2/k=40, `Store.Lookup` and
  `Store.Add` make up 2.09 s of the 2.59 s of CPU samples under `check.run`
  (81%). For B3 the figure is 0.67 s of 0.97 s (69%) [P]. Within that, **map growth** (`table.rehash`)
  is 0.67 s in B2 and 0.13 s in B3. Lookups that miss or hit cost 0.92 s
  and 0.31 s [P].
- **The engine allocates about three objects per admitted state**: the key
  string, the emit closure (`check/run.go:118`), and the `guard`
  (`check/run.go:116`). Everything else is model allocation or slice/map
  growth [P][D]. Measured allocs/state is 2.97–3.02 for B3, B7, and V1 [M].
  Their states are fixed-size values, so the models allocate nothing per
  successor (code reading).
- **Some of the per-state cost is the model's own code.** In B5, `fmt.Sprintf` for action
  labels is 0.40 s of 1.04 s engine samples and 62% of sampled allocated
  objects [P], and the engine discards those labels during search. In B2, GridN's
  per-successor slice copy is 63% of sampled objects [P].
- **GC uses a lot of CPU but adds little wall time on this machine.** In
  B2/k=40, background mark workers account for 28% of CPU samples [P], but the
  runtime's GC CPU fraction is 2.7% of the 16 available threads [M]. Total
  stop-the-world time was 4.49 ms in a 2,677 ms run [M, GC trace]. The
  indirect cost (write barriers, cache competition) is not measured.
- **Trace reconstruction is negligible** for the deep counterexample: about
  40 µs of 153–159 ms in B7 [M].
- **State size matters.** At identical state and transition counts, B8 with
  a 256-byte payload takes 1.66× the time of B8 with a 64-byte payload and
  allocates 2.42× the bytes [M]. No B8 profile exists, so the cost is not
  attributed to specific functions.
- **Number of states.** Allocation volume and allocs/state are flat with
  size in every family [M]. In the B2/B8 families, time grows in proportion
  to states from 10k to 160k states. From 160k to 2.56M states it grows
  1.37–1.47× faster than the state count, with non-overlapping run ranges [M].
  The cause is not established, because only the largest size was profiled.
  B5 is proportional over its two largest sizes. B6 is too small to
  characterize.

## 2. Evidence and provenance

**Measurements.** `A.txt` and `B.txt` in
`~/atlas-bench-data/2026-10-06-ryzen7-7435hs-wsl2/`, as summarized in sections 4,
5, 7, and 8 of the baseline report. The extra derived figures below (B3, B7,
B8, V1 rows, growth ratios, and RSS per state) were computed from those two
files with the same method as the baseline's `summarize.py`: the median of
all 20 runs, divided by the checked counts.

**Profiles.** All files in the baseline report's section 9 inventory exist
in `profiles/`, and no files are missing or extra. All of them open with `go tool pprof`
(go1.26.1) against `bench.test`:

| Workload | Files | Profile run (`bench-*.txt`) | CPU samples |
|---|---|---|---|
| B2/k=40 | `cpu-B2-k_40.out`, `mem-B2-k_40.out` | b.N=1, 2.66 s/op | 3.68 s over 2.67 s (368 samples) |
| B3 | `cpu-B3.out`, `mem-B3.out` | b.N=10, 102 ms/op | 1.30 s over 1.02 s |
| B5/n=6 | `cpu-B5-n_6.out`, `mem-B5-n_6.out` | b.N=20, 54.6 ms/op | 1.31 s over 1.10 s |
| B6/cap=4 | `cpu-B6-cap_4.out`, `mem-B6-cap_4.out` | b.N=5906, 188 µs/op | 1.33 s over 1.11 s |
| B2/k=40 GC | `gctrace-B2-k_40.log`, `gctrace-B2-k_40.kv` | one `atlasbench` run, 2.68 s | — |

Views used: `-top`, `-top -cum`, `-peek`, `-list` on the CPU profiles;
`-sample_index=alloc_space`, `alloc_objects`, and `inuse_space` on the memory
profiles.

**Provenance checks.**
- The baseline tree was `fb4bbdb` plus uncommitted Phase 3 batch 4 changes.
  `git diff fb4bbdb -- check core models` is empty today, and the source
  lines that `pprof -list` attributes in `check/run.go`, `core/store.go`, and
  `models/models.go` match the current files.
- The `bench-*.txt` headers say `atlas-commit: unknown`. This is expected
  from `internal/bench/env.go`: a test binary has no VCS stamp, and
  `bench.test` was run from `profiles/`, outside the repository, so the git
  fallback found nothing. The connection to the baseline tree rests on
  timestamps instead. Every file under `internal/bench/` was last modified at
  or before 22:39 local time on 2026-10-06. The `atlasbench` binary was built
  at 22:40, the sets ran 22:40–22:44, and the profiles were taken at 22:45. This
  is consistent with the baseline report, but it is weaker evidence than a
  commit hash.
- The memory profiles agree with the measured totals: B2/k=40 `alloc_space`
  is 1,131.5 MiB, and the profile run's `B/op` is 1,120.9 MiB. Object counts
  in the profile are sampled estimates (the default `MemProfileRate`) and
  run up to 12% below `allocs/op`. Profile percentages, not absolute object
  counts, are used below.

## Processing per state

### 3.1 Where the engine goroutine's CPU goes [P]

The columns are seconds of CPU samples, with the percentage of the samples
under `check.(*engine).run` in parentheses. "Model own" is the model's
`Next` minus the emit callback (`emitNext`). It is model code, not engine
code. GC mark is reported separately because it runs on other threads
(section 3.6).

| Component (function) | B2/k=40 | B3 | B5/n=6 | B6/cap=4 |
|---|---|---|---|---|
| `check.run` (cum) | 2.59 | 0.97 | 1.04 | 1.08 |
| Visited lookup (`Store.Lookup` → `mapaccess2_faststr`) | 0.92 (36%) | 0.31 (32%) | 0.12 (12%) | 0.26 (24%) |
| Visited insert (`Store.Add`) | 1.17 (45%) | 0.36 (37%) | 0.19 (18%) | 0.22 (20%) |
| — of which map insert, incl. growth (`store.go:43`) | 0.70 | 0.21 | 0.08 | 0.12 |
| — of which map growth (`maps.(*table).rehash`) | 0.67 | 0.13 | 0.06 | 0.10 |
| — of which slice appends (`store.go:44–47`) | 0.46 | 0.12 | 0.06 | 0.06 |
| Key encoding (`engine.key` → `AppendKey`) | 0.02 (1%) | 0.01 (1%) | 0.09 (9%) | 0.18 (17%) |
| Model own (`Next` − `emitNext`) | 0.28 (11%) | 0.13 (13%) | 0.46 (44%) | 0.20 (19%) |
| — of which `fmt.Sprintf` (action labels) | — | — | 0.40 (38%) | — |
| Invariants (`engine.violated`) | none configured | none configured | 0.04 (4%) | not sampled |
| Queue (`Queue.Push` / `Pop`) | 0.02 | 0.06 | 0.03 | 0.04 / 0.02 |
| GC background mark (`gcBgMarkWorker`, other threads) | 1.02 | 0.24 | 0.16 | 0.12 |

Limits on this table: sampling is at 100 Hz, so an entry of 0.05 s is about 5
samples. Entries below about 0.05 s cannot be told apart from each other.
Percentages hold only for these four workloads. The B6 profile covers 5,906
repetitions of a 315-state run in one warm process. It describes the
small-run regime, in which each run regrows an empty map, more than it
describes per-state work at scale.

### 3.2 Visited-set lookup [P][I]

- `Store.Lookup` is called once per transition: 9.98M times in B2/k=40 and
  483,840 times in B3. It is the largest single component in B2 and B3.
- In B2/k=40, all 0.58 s of `maps.ctrlGroup.matchH2` (15.8% of all samples, flat) comes from
  `mapaccess2_faststr`. **[H]** The map holds 2.56M entries, far more than the
  CPU's caches can hold. Cache misses on control words are a plausible reason
  that probing appears as flat time in `matchH2`. Hardware counters were not
  collected, so this is not verified.
- B3 spends 0.09 s (6.9%) in `memeqbody`, called from the map. This is the
  exact key comparison on lookup hits. It is required by the identity rule
  (AGENTS.md: "a hash is not identity"). It can be made cheaper, but not
  removed.

### 3.3 Store growth: map rehash and slice regrowth [P]

- `aeshashbody` is 0.58 s in B2/k=40. **0.52 s of it comes from
  `maps.(*table).split`**, which rehashes the stored keys when the map grows.
  Only 0.05 s comes from lookups. Map growth (`rehash`) is 26% of B2's
  engine samples and 13% of B3's.
- `runtime.growslice` called from `Store.Add` is 0.39 s in B2/k=40. It
  spends 0.29 s in `memmove` and 0.06 s in `bulkBarrierPreWriteSrcOnly`
  (copying a pointer-holding `[]string` while GC is marking). By source
  line, `s.keys = append(s.keys, k)` (`store.go:44`) costs 0.34 s.
- **[I]** These are amortized growth costs of an unsized `map[string]StateID` and four
  parallel slices (D-003 baseline layout). The state count is not known in
  advance, so pre-sizing is not available in general.

### 3.4 Allocations [P][M]

Shares of `alloc_space` (bytes) and `alloc_objects` (count) by source line:

| Site | B2/k=40 bytes / objects | B3 bytes / objects | B5/n=6 bytes / objects | B6/cap=4 bytes / objects |
|---|---|---|---|---|
| Model successor copy (`models.go:89`, GridN) | 26% / 63% | — | — | — |
| `fmt.Sprintf` in `TwoPhase.Next` | — | — | 19% / 62% | — |
| Map insert, growth (`store.go:43`) | 19% / <1% | 21% / <1% | 16% / <1% | 25% / 1% |
| `keys` append (`store.go:44`) | 18% / 0 | 24% / 0 | 21% / 0 | 18% / 1% |
| `parent`/`edge`/`depth` appends (`store.go:45–47`) | 13% / 0 | 21% / 0 | 14% / 0 | 11% / 3% |
| Key string (`store.go:42`) | 2% / 11% | 5% / 43% | 3% / 12% | 4% / 31% |
| Emit closure (`run.go:118`) | 18% / 17% | 18% / 38% | 15% / 14% | 17% / 36% |
| `guard` (`run.go:116`) | 2% / 9% | 2% / 18% | 2% / 8% | 3% / 23% |
| Queue growth (`queue.go:13`) | 1% / 0 | 8% / 0 | 6% / 0 | 20% / 2% |

- **[D]** The measured allocs/state matches this breakdown. Per admitted
  state, the engine makes one key string, one closure, and one guard, which
  is 3 objects. GridN adds one copy per transition (3.90 transitions/state),
  so the expected total is 6.9, and B2/k=40 measures 6.91 [M]. B3, B7, and
  V1 measure 3.01, 2.97, and 3.02 [M]. B5/n=6 measures 7.97: the engine's 3
  plus about one `Sprintf` per labeled transition.
- Bytes are dominated by `Store.Add` (map, slices, and key strings): 52–71%
  of bytes for B2, B3, B5, and B6. Object counts come from per-state and
  per-transition allocations.
- `inuse_space` is not informative. The memory profiles were written after
  the benchmark returned, so they show only about 1–5 MiB of runtime
  residue. **There is no live-heap profile of a full store.**

### 3.5 Successor generation, key encoding, and invariants [P]

- The cost of successor generation and key encoding depends on the model. It
  is 1% of engine samples for B2/B3 keys (raw byte copies), but 17% for
  B6, whose `ABP.AppendKey` writes varints (`encoding/binary`).
- B5's `TwoPhase.Next` formats an action label with `fmt.Sprintf` for every
  RM transition (`internal/bench/twophase.go`). The engine discards actions
  during search (`check/run.go:118`, `func(_ A, s S)`) and uses them only in
  traces. **[I]** B5's per-state numbers therefore measure `fmt` as much as
  the engine. This is a property of the benchmark model, not an engine
  bottleneck.
- Invariant checking is small wherever it is configured: 0.04 s (4%) in B5
  and not sampled at all in B6.

### 3.6 Garbage collection [M][P]

Three sources, which measure different things:

| Source (B2/k=40) | What it measures | Value |
|---|---|---|
| CPU profile, `gcBgMarkWorker` (cum) | share of CPU samples on all threads | 1.02 s of 3.68 s (28%) |
| `gc-cpu-fraction` ([M], runtime/metrics) | GC CPU ÷ (wall × GOMAXPROCS=16) | median 0.0267 (A) / 0.0273 (B) |
| `gctrace-B2-k_40.log` (27 cycles) | stop-the-world pause, sum (max) | 4.49 ms (0.46 ms) |
| same | mark-assist CPU on the search goroutine | 19.8 ms |
| same | background + idle mark CPU | 490 + 516 ms |
| same | wall time with a concurrent mark running | 260 ms of 2,677 ms |
| same | last cycle: heap before → live after | 355 MB → 249 MB |

- **[D]** The two CPU figures agree: 1.02 s ÷ (2.67 s × 16) = 2.4%, against
  a measured 2.7%. The background mark CPU in the GC trace, about 1.0 s, also
  matches the profile, although it comes from a different process.
- **[I]** On this 16-thread machine, GC runs mostly on otherwise idle cores.
  Its direct cost to the search goroutine (stop-the-world plus assists) is
  about 0.9% of wall time in the traced run. Write barriers, which are
  active during about 10% of wall time, and competition for cache and memory
  bandwidth are not separable with this evidence.
- **[H]** On fewer cores (`GOMAXPROCS=1`) or on a loaded machine, GC would
  compete directly with the search. This was not measured. The mark work
  grows with the pointer-holding live heap. `map[string]StateID` and
  `[]string` hold one key string per state, which is the GC pressure D-003
  anticipated.

### 3.7 Trace reconstruction [M]

- B7 (trace length 21): the per-run difference between time to first
  counterexample and violation detection has a median of 41.8 µs (A) and
  38.7 µs (B), against a total of 159 ms and 153 ms. That is under 0.03%.
- B4: 15.3 µs and 14.8 µs of 66 µs and 62 µs. B4 is a noisy microsecond
  workload (section 5).
- No B4 or B7 profile was collected (BENCHMARKS.md §6 does not require
  one). For B7, the time to first counterexample is the time to explore
  166,581 states breadth-first to depth 21. That is a number-of-states
  effect, not a trace cost.

### 3.8 State size (B8) [M][D]

B8 holds states and transitions fixed and varies only the payload:

| k | ns/state, pad=64 → pad=256 | Alloc B/state | Peak RSS MiB |
|---|---|---|---|
| 10 | 935 → 1,461 (×1.56) | 676 → 1,633 | 8.6 → 10.9 |
| 20 | 1,089 → 1,806 (×1.66) | 719 → 1,717 | 46.4 → 103.3 |
| 40 | 1,495 → 2,478 (×1.66) | 719 → 1,738 | 638.0 → 1,425.0 |

- **[D]** At k=40, the extra 192 bytes per state cost about 984 ns per state.
  That is about 252 ns per transition, or about 1.3 ns per extra byte per
  transition. Allocs/state is identical (6.91).
- No B8 profile exists, so this cost cannot be divided among copying,
  hashing, key comparison, and GC. B2 is not B8 with pad=0: GridN states
  are `[]int` with 4-byte keys, while B8 states are `[]byte`. The two are
  not compared directly.

## Number of states

### 4.1 Measured growth [M][D]

Medians of 20 runs. RSS/state subtracts B1's median RSS (4.12 MiB), which is
the process baseline.

| Workload | States | Trans./state | ns/state | ns/trans. | Alloc B/state | Allocs/state | Peak RSS MiB | RSS B/state | GC cycles |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| B2/k=10 | 10,000 | 3.60 | 746 | 207 | 430 | 6.63 | 8.1 | 419 | 1 |
| B2/k=20 | 160,000 | 3.80 | 699 | 184 | 464 | 6.81 | 31.0 | 176 | 13 |
| B2/k=40 | 2,560,000 | 3.90 | 1,025 | 263 | 459 | 6.91 | 393.7 | 160 | 27 |
| B5/n=3 | 288 | 2.64 | 938 | 355 | 404 | 5.84 | 4.2 | — | 0 |
| B5/n=4 | 1,568 | 3.43 | 908 | 265 | 370 | 6.36 | 4.7 | — | 0 |
| B5/n=5 | 8,832 | 4.23 | 1,222 | 289 | 406 | 7.15 | 7.7 | 423 | 1 |
| B5/n=6 | 50,816 | 5.04 | 1,219 | 242 | 415 | 7.97 | 13.6 | 195 | 6 |
| B6/cap=2 | 81 | 3.69 | 1,145 | 310 | 458 | 4.36 | 4.2 | — | 0 |
| B6/cap=3 | 172 | 4.51 | 920 | 204 | 378 | 3.68 | 4.1 | — | 0 |
| B6/cap=4 | 315 | 5.01 | 888 | 177 | 374 | 3.39 | 4.2 | — | 0 |

Step ratios (time ÷ states ratio):

| Step | States × | Transitions × | Time × | RSS × |
|---|---:|---:|---:|---:|
| B2 k=10 → 20 | 16.00 | 16.89 | 14.99 | 3.82 |
| B2 k=20 → 40 | 16.00 | 16.42 | 23.47 | 12.70 |
| B8/pad=64 k=20 → 40 | 16.00 | 16.42 | 21.95 | 13.74 |
| B8/pad=256 k=20 → 40 | 16.00 | 16.42 | 21.96 | 13.79 |
| B5 n=4 → 5 | 5.63 | 6.95 | 7.58 | 1.64 |
| B5 n=5 → 6 | 5.75 | 6.86 | 5.74 | 1.76 |
| B6 cap=2 → 3 | 2.12 | 2.59 | 1.71 | 0.97 |
| B6 cap=3 → 4 | 1.83 | 2.03 | 1.77 | 1.03 |

### 4.2 Interpretation

- **Total work is proportional to states in allocation terms.** Alloc
  B/state and allocs/state are flat within each family (B2: 430–464 B and
  6.63–6.91), and transitions/state changes by at most 8% across B2 sizes.
  The work per state, as counted by allocations and transitions, does not
  depend on how many states there are.
- **B2/B8, from 10k to 160k states:** time grows slightly less than states
  (×14.99 for ×16). The per-state ranges overlap (B2: 692–815 against
  662–751 ns/state), so this is consistent with proportional growth.
- **B2/B8, from 160k to 2.56M states: superlinear between these two sizes.**
  Time grows ×22–23.5 for ×16 states. B2 goes from 699 to 1,025 ns/state
  (+47%), and the ranges do not overlap: 662–751 against 989–1,074 ns/state
  over all 20 runs. Both sets show it (A 688 → 1,033; B 713 → 1,020). It
  occurs at the same allocs/state, so it is not caused by more allocation
  per state. With three sizes, no growth exponent can be fitted, and these
  data cannot show whether the trend continues.
- **[H] Possible causes of the per-state increase, none verified:** cache
  and TLB misses as the visited map and key strings outgrow the caches
  (`matchH2` flat time at k=40, section 3.2); map growth at larger table
  sizes; GC marking a larger live heap (27 against 13 cycles). **Only k=40
  was profiled, so no component can be shown to have grown.** This is the
  main gap in this analysis.
- **B5:** from n=5 to n=6, time grows ×5.74 for ×5.75 states, so it is
  proportional to states. The per-transition cost falls (289 → 242 ns) as
  branching rises. The n=3 and n=4 runs take under 1.5 ms and are noisy
  (n=3 ±48%; n=4 is one of the three set disagreements), so the n=4 → 5 step
  (×7.58) is not interpreted.
- **B6: inconclusive.** All sizes take under 0.3 ms, have no GC cycles,
  run at the process-baseline RSS, and have spreads of ±6% to ±81%. Time
  grows less than states, which is consistent with fixed per-run costs
  dominating. These sizes cannot characterize growth. Larger B6 sizes would
  need a new suite version (BENCHMARKS.md §3).
- **Memory:** at 160k states and above, RSS per state is roughly constant
  within a family: B2 176 → 160 B, B8/pad=64 277 → 260 B, B8/pad=256 650 →
  582 B. So peak RSS grows in proportion to states over the measured range.
  Peak RSS is not live data. With the default `GOGC`, the heap reaches about
  twice its live size before a collection (355 MB before against 249 MB live
  in the last B2/k=40 cycle).
- Per-state cost and state-space size are separate questions. B2/k=40 is
  slow mostly because it has 2.56M states and 9.98M transitions. Its
  per-state cost is about 1.47× that of k=20.

## 5. Noisy, inconclusive, or missing evidence

- **Noisy workloads [M]:** B1, B4, B5/n=3, and B6/cap=2–4 have `sec/op`
  spreads of ±16% to ±81% and outliers up to 3.5× the median. Their times
  support no per-state conclusions.
- **Set disagreements [M]:** B2/k=20 (+3.68%, p=0.019), B5/n=4 (−2.83%,
  p=0.043), and B8/pad=64/k=40 (−3.32%, p=0.023). Differences of about 4%
  are not resolved on this machine. The B2/k=20 → k=40 increase of 47% is
  far larger than that.
- **Profiles cover only B2/k=40, B3, B5/n=6, and B6/cap=4**, as §6
  requires. There are no profiles for B7, B8, V1, or any intermediate size.
  The profiles are `testing.B` runs in a warm process. They are not the
  one-shot `atlasbench` runs. For B3, B5, and B6, the profiled `ns/op` was
  lower than the one-shot medians (B3 102 ms against 131 ms; B6 188 µs
  against 280 µs). **[H]** First-run heap growth and page faults in a fresh
  process may explain the difference. This was not verified.
- **Profiling overhead:** the CPU and memory profilers were both on during
  the profile runs. The B2/k=40 profile run took 2.66 s, against the 2.62 s
  set median.
- **Not recorded:** peak Go heap (BENCHMARKS.md §2; the driver records RSS
  instead), the "largest size within a stated memory limit" (§7; no limit
  was stated), hardware counters, a live-heap profile of a full store, and
  any measurement at `GOMAXPROCS=1`.
- The reference explorer was not run. It is a test oracle and is not part
  of these measurements.
- **No supplementary profile was collected.** The gap in section 4.2 is
  handed to Phase 4 as candidate 1.

## 6. Phase 4 investigation candidates (proposals only)

Every candidate keeps the current implementation as the correctness
reference, changes one factor, and is judged by `benchstat` A/B results on
the fixed suite with V1 held out (ROADMAP Phase 4). No speedup is promised.

1. **Attribute the per-state growth before changing anything.**
   *Evidence:* the B2/B8 per-state cost rises 37–47% between 160k and 2.56M
   states (4.2), and only k=40 was profiled. *Benefit:* tells whether the
   visited-set layout or GC is the right target at scale. *Risk:* none
   (measurement only). *Method:* CPU and memory profiles of B2/k=20, B2/k=40,
   and B8/pad=256/k=40 from one tree; one `GODEBUG=gctrace=1` run per size;
   one `GOMAXPROCS=1` set. Optionally add peak heap to `atlasbench`.
   *Success:* each component's share at k=20 against k=40 is known.
2. **Visited-set layout (D-003, optimized layout Unresolved).** *Evidence:*
   lookup plus insert is 81% of B2 and 69% of B3 engine samples; map growth
   alone is 26% and 13%; rehashing re-reads every stored key (3.2–3.3); one
   key string is allocated per state. *Benefit:* possibly lower ns/state,
   B/state, and GC mark work. *Risk:* **high**. Deduplication must stay exact,
   a hash match must be confirmed by a full key comparison, and IDs must stay
   dense and in admission order. *Tests:* a differential test that feeds the
   new and current `Store` the same key streams (including forced hash
   collisions) and requires identical IDs and Lookup results; all
   conformance and reference-explorer differential tests. *Microbenchmark:*
   `Store` Lookup/Add on recorded B2/k=40 and B3 key streams. *Success:*
   lower ns/state and B/state on B2/k=40 and B3, beyond benchstat noise, with
   every verdict and count unchanged. V1 must not regress.
3. **Per-expansion allocations in `check.run`.** *Evidence:* the emit
   closure (`run.go:118`) and the `guard` (`run.go:116`) are 2 of the
   engine's 3 allocs per state: 38% plus 18% of sampled objects in B3, and
   about 20% of bytes in B2 (3.4). *Benefit:* about 2 fewer allocs per state
   on every workload; the time effect is unknown. *Risk:* **medium**. The
   per-call `guard` is what detects a model that keeps `emit` and calls it
   after `Next` returns (D-001). A reused emitter needs a per-call
   generation check. *Tests:* the existing callback-contract tests (late
   emit, emit after false), plus a new test that keeps an emitter from one
   expansion and calls it during a later one. *Microbenchmark:*
   `testing.AllocsPerRun` on a fixed small model. *Success:* allocs/state
   falls by about 2 with identical results; ns/state is reported either way.
4. **Store and queue slice regrowth.** *Evidence:* `growslice` from
   `Store.Add` takes 0.39 s in B2/k=40, and the appends are 31–45% of bytes
   in B2, B3, and B5 (3.3–3.4). *Benefit:* fewer copied bytes and less
   pointer-slice write-barrier work. *Risk:* low (indexing only). *Tests:*
   the existing `core` tests plus `Parent`/`Edge`/`Depth`/`Key` checks across
   chunk boundaries. *Success:* lower B/op on B2 and B3 without a time
   regression. Isolate this from candidate 2.
5. **Decide what the model-side costs in the suite should measure.**
   *Evidence:* `Sprintf` is 38% of B5 engine samples and 62% of its sampled
   objects, and GridN's copies are 63% of B2's sampled objects (3.4–3.5).
   *Benefit:* B5 would measure the engine. *Risk:* changing a model creates
   suite version 2, which is not comparable with this baseline. Lazy actions
   would change the public API (D-001) and need a DECISIONS.md entry. This is
   an owner decision, not an engine optimization.

## 7. Phase 3 exit criteria

| Criterion (ROADMAP Phase 3) | Evidence | Assessment |
|---|---|---|
| Every benchmark checks its semantic result | `Want.Check` runs on every iteration (`internal/bench/bench_test.go` `runChecked`) and in every `atlasbench` run. All 21 workloads in `workloads.go` set status, states, and transitions; B4 and B7 also set the invariant, trace length, and replay. `TestBenchmarkFailsOnWrongCount`, `TestWrongResultFails`, and `TestWorkloads` pass. All 420 baseline runs passed. | Met |
| Two independent runs agree within the noise recorded by `benchstat` | 18 of 21 `sec/op` comparisons show `~`. Three differ at p < 0.05 by 2.8–3.7%; each lies within its own ± spread but is flagged by the test. Peak RSS, B/op, and GC cycles show no differences. D-007 says that `benchstat`'s statistical comparison decides and that no tolerance replaces it. | **Not met** (owner ruling, 2026-10-14). Phase 3 stays open until the sets agree |
| A written report names the top bottlenecks with profile evidence, in separate sections | This document: "Processing per state" and "Number of states" | Met |
