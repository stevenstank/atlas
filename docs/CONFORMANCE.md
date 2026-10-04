# Conformance models

> Status: hand-worked expectations, written before any engine code exists.
> Phase 1 turns each section into a test. If the engine disagrees with this
> document, first re-check the arithmetic here, then fix whichever is wrong.

The first conformance model, `Grid2`, is in [SEMANTICS.md §6](SEMANTICS.md#6-search-order-and-the-shortest-counterexample-guarantee).
This document adds the water-jug model.

## Water jugs (3 L / 5 L)

### Model definition

These conventions are fixed. Any change to them changes the expected numbers.

- **State:** `(s, b)`, the litres in the small jug (capacity 3) and the big jug
  (capacity 5). Both are integers.
- **Key:** two bytes, `s` then `b`.
- **Initial states:** `[(0,0)]`.
- **Actions**, emitted in this order. An action is emitted **only if it changes
  the state** (no self-loops):

| # | Action       | Enabled when       | Result |
|---|--------------|--------------------|--------|
| 1 | `FillSmall`  | `s < 3`            | `(3, b)` |
| 2 | `FillBig`    | `b < 5`            | `(s, 5)` |
| 3 | `EmptySmall` | `s > 0`            | `(0, b)` |
| 4 | `EmptyBig`   | `b > 0`            | `(s, 0)` |
| 5 | `SmallToBig` | `s > 0 && b < 5`   | `a = min(s, 5−b)`; `(s−a, b+a)` |
| 6 | `BigToSmall` | `b > 0 && s < 3`   | `a = min(b, 3−s)`; `(s+a, b−a)` |

- **Invariant:** `NotFour: b != 4`.

### Full BFS expansion (no invariant)

`D` means a new state was discovered. `dup` means the target was already
visited. Rows are listed in expansion order, which equals discovery order.

| # | Expand | Depth | Steps emitted (in order)                                                    | Tr | Dup |
|---|--------|------:|-----------------------------------------------------------------------------|---:|----:|
| 1 | (0,0)  | 0 | FillSmall→(3,0) D · FillBig→(0,5) D                                             | 2 | 0 |
| 2 | (3,0)  | 1 | FillBig→(3,5) D · EmptySmall→(0,0) dup · SmallToBig→(0,3) D                     | 3 | 1 |
| 3 | (0,5)  | 1 | FillSmall→(3,5) dup · EmptyBig→(0,0) dup · BigToSmall→(3,2) D                   | 3 | 2 |
| 4 | (3,5)  | 2 | EmptySmall→(0,5) dup · EmptyBig→(3,0) dup                                       | 2 | 2 |
| 5 | (0,3)  | 2 | FillSmall→(3,3) D · FillBig→(0,5) dup · EmptyBig→(0,0) dup · BigToSmall→(3,0) dup | 4 | 3 |
| 6 | (3,2)  | 2 | FillBig→(3,5) dup · EmptySmall→(0,2) D · EmptyBig→(3,0) dup · SmallToBig→(0,5) dup | 4 | 3 |
| 7 | (3,3)  | 3 | FillBig→(3,5) dup · EmptySmall→(0,3) dup · EmptyBig→(3,0) dup · SmallToBig→(1,5) D | 4 | 3 |
| 8 | (0,2)  | 3 | FillSmall→(3,2) dup · FillBig→(0,5) dup · EmptyBig→(0,0) dup · BigToSmall→(2,0) D | 4 | 3 |
| 9 | (1,5)  | 4 | FillSmall→(3,5) dup · EmptySmall→(0,5) dup · EmptyBig→(1,0) D · BigToSmall→(3,3) dup | 4 | 3 |
| 10 | (2,0) | 4 | FillSmall→(3,0) dup · FillBig→(2,5) D · EmptySmall→(0,0) dup · SmallToBig→(0,2) dup | 4 | 3 |
| 11 | (1,0) | 5 | FillSmall→(3,0) dup · FillBig→(1,5) dup · EmptySmall→(0,0) dup · SmallToBig→(0,1) D | 4 | 3 |
| 12 | (2,5) | 5 | FillSmall→(3,5) dup · EmptySmall→(0,5) dup · EmptyBig→(2,0) dup · BigToSmall→(3,4) D | 4 | 3 |
| 13 | (0,1) | 6 | FillSmall→(3,1) D · FillBig→(0,5) dup · EmptyBig→(0,0) dup · BigToSmall→(1,0) dup | 4 | 3 |
| 14 | (3,4) | 6 | FillBig→(3,5) dup · EmptySmall→(0,4) D · EmptyBig→(3,0) dup · SmallToBig→(2,5) dup | 4 | 3 |
| 15 | (3,1) | 7 | FillBig→(3,5) dup · EmptySmall→(0,1) dup · EmptyBig→(3,0) dup · SmallToBig→(0,4) dup | 4 | 4 |
| 16 | (0,4) | 7 | FillSmall→(3,4) dup · FillBig→(0,5) dup · EmptyBig→(0,0) dup · BigToSmall→(3,1) dup | 4 | 4 |

**Reachable states by depth**

| Depth | States |
|------:|--------|
| 0 | (0,0) |
| 1 | (3,0), (0,5) |
| 2 | (3,5), (0,3), (3,2) |
| 3 | (3,3), (0,2) |
| 4 | (1,5), (2,0) |
| 5 | (1,0), (2,5) |
| 6 | (0,1), (3,4) |
| 7 | (3,1), (0,4) |

The 16 reachable states are exactly the states where at least one jug is
empty or full: 12 with `s ∈ {0,3}`, plus `(1,0), (2,0), (1,5), (2,5)`. This
gives an independent check of the count.

### Expected results

| Test | Configuration | Status | Discovered | Expanded | Transitions | Duplicates | Terminal | Max depth |
|------|---------------|--------|-----------:|---------:|------------:|-----------:|---------:|----------:|
| J1 | no invariant | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 7 |
| J2 | `NotFour` | `Violation` | 14 | 12 | 42 | 29 | 0 | 6 |

The statistics identity holds in each row:
`transitions = (discovered − 1) + duplicates`. For J1: 58 = 15 + 43. For J2:
42 = 13 + 29.

**J2 counterexample** (6 steps). The run stops while expanding row 12, at its
4th step:

```
(0,0) -FillBig-> (0,5) -BigToSmall-> (3,2) -EmptySmall-> (0,2)
      -BigToSmall-> (2,0) -FillBig-> (2,5) -BigToSmall-> (3,4)
```

This trace is minimal. The only states with `b = 4` are `(3,4)` at depth 6 and
`(0,4)` at depth 7.

**Depth-bound tests (J3, J4) depend on [D-012](DECISIONS.md), which is
unresolved.** Under option (a), states at depth D are never expanded. Under
option (b), they are expanded, but new successors are counted as `CutOff` and
not discovered.

| Test | Bound | Option | Status | Discovered | Expanded | Transitions | Duplicates | CutOff |
|------|------:|--------|--------|-----------:|---------:|------------:|-----------:|-------:|
| J3a | 6 | (a) | `Bounded` | 14 | 12 | 42 | 29 | — |
| J3b | 6 | (b) | `Bounded` | 14 | 14 | 50 | 35 | 2 |
| J4a | 7 | (a) | `Bounded` (every state found, but this cannot be known) | 16 | 14 | 50 | 35 | — |
| J4b | 7 | (b) | `Exhausted`, same as J1 | 16 | 16 | 58 | 43 | 0 |

In J3b, the cut-off successors are `(3,1)` (from `(0,1)`) and `(0,4)` (from
`(3,4)`). Neither is discovered or checked against invariants. The identity
under (b) is `transitions = (discovered − 1) + duplicates + cutoff`, which
gives 50 = 13 + 35 + 2.

### Encoding sensitivity

If every action is always emitted (self-loops included, as in the classic TLA+
`DieHard` spec), the distinct-state count stays 16, but transitions become
16 × 6 = 96 and duplicates become 81. Benchmark comparisons with other tools
must compare **distinct states**, and must state each tool's transition
convention ([BENCHMARKS.md §9](BENCHMARKS.md#9-comparisons-with-other-tools)).

### Provenance

Worked by hand on 2026-10-04, then cross-checked with a throwaway BFS script
kept outside the repository. The script caught one arithmetic slip in the
hand pass (row 6, `SmallToBig`). This table reflects the corrected values.
