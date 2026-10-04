# Conformance models

> Status: hand-worked expectations, written before any engine code exists.
> Phase 1 turns each section into a test. If the engine disagrees with this
> document, first re-check the arithmetic here, then fix whichever is wrong.

The first conformance model, `Grid2`, is in [SEMANTICS.md §6](SEMANTICS.md#6-search-order-and-the-shortest-counterexample-guarantee).
This document adds the water-jug model.

## Water jugs (3 L / 5 L)

### Model definition

These conventions are fixed. Changing any of them changes the expected
numbers.

- **State:** `(s, b)`, the litres in the **small** jug (capacity 3) followed
  by the **big** jug (capacity 5). Both are integers.
- **Key:** two bytes, `s` then `b`.
- **Initial states:** `[(0,0)]`.
- **Actions**, emitted in exactly this order. An action is emitted **only if
  it changes the state**, so there are no self-loops:

| # | Action       | Enabled when       | Result |
|---|--------------|--------------------|--------|
| 1 | `FillBig`    | `b < 5`            | `(s, 5)` |
| 2 | `FillSmall`  | `s < 3`            | `(3, b)` |
| 3 | `EmptyBig`   | `b > 0`            | `(s, 0)` |
| 4 | `EmptySmall` | `s > 0`            | `(0, b)` |
| 5 | `BigToSmall` | `b > 0 && s < 3`   | `a = min(b, 3−s)`; `(s+a, b−a)` |
| 6 | `SmallToBig` | `s > 0 && b < 5`   | `a = min(s, 5−b)`; `(s−a, b+a)` |

- **Invariant:** `NotFour: b != 4`. A state violates it when the big jug holds
  exactly 4 L.

> **Revision note (2026-10-04).** An earlier version of this document used the
> order FillSmall, FillBig, EmptySmall, EmptyBig, SmallToBig, BigToSmall. The
> owner has specified the order above. Totals for the exhaustive run (states,
> transitions, duplicates, max depth, dead ends) do not depend on order, and
> they were re-verified unchanged. The expansion table and the statistics at
> the point a violation stops the run *do* depend on order, and have changed.
> The earlier values were correct for the earlier order. The 6-step
> counterexample is the same under both orders.

### Full BFS expansion (no invariant)

`D` means a new state was discovered. `dup` means the target was already
visited. Rows are in expansion order, which equals discovery order.

| # | Expand | Depth | Steps emitted (in order) | Tr | Dup |
|--:|--------|------:|--------------------------|---:|----:|
| 1 | (0,0) | 0 | FillBig→(0,5) D · FillSmall→(3,0) D | 2 | 0 |
| 2 | (0,5) | 1 | FillSmall→(3,5) D · EmptyBig→(0,0) dup · BigToSmall→(3,2) D | 3 | 1 |
| 3 | (3,0) | 1 | FillBig→(3,5) dup · EmptySmall→(0,0) dup · SmallToBig→(0,3) D | 3 | 2 |
| 4 | (3,5) | 2 | EmptyBig→(3,0) dup · EmptySmall→(0,5) dup | 2 | 2 |
| 5 | (3,2) | 2 | FillBig→(3,5) dup · EmptyBig→(3,0) dup · EmptySmall→(0,2) D · SmallToBig→(0,5) dup | 4 | 3 |
| 6 | (0,3) | 2 | FillBig→(0,5) dup · FillSmall→(3,3) D · EmptyBig→(0,0) dup · BigToSmall→(3,0) dup | 4 | 3 |
| 7 | (0,2) | 3 | FillBig→(0,5) dup · FillSmall→(3,2) dup · EmptyBig→(0,0) dup · BigToSmall→(2,0) D | 4 | 3 |
| 8 | (3,3) | 3 | FillBig→(3,5) dup · EmptyBig→(3,0) dup · EmptySmall→(0,3) dup · SmallToBig→(1,5) D | 4 | 3 |
| 9 | (2,0) | 4 | FillBig→(2,5) D · FillSmall→(3,0) dup · EmptySmall→(0,0) dup · SmallToBig→(0,2) dup | 4 | 3 |
| 10 | (1,5) | 4 | FillSmall→(3,5) dup · EmptyBig→(1,0) D · EmptySmall→(0,5) dup · BigToSmall→(3,3) dup | 4 | 3 |
| 11 | (2,5) | 5 | FillSmall→(3,5) dup · EmptyBig→(2,0) dup · EmptySmall→(0,5) dup · BigToSmall→(3,4) D | 4 | 3 |
| 12 | (1,0) | 5 | FillBig→(1,5) dup · FillSmall→(3,0) dup · EmptySmall→(0,0) dup · SmallToBig→(0,1) D | 4 | 3 |
| 13 | (3,4) | 6 | FillBig→(3,5) dup · EmptyBig→(3,0) dup · EmptySmall→(0,4) D · SmallToBig→(2,5) dup | 4 | 3 |
| 14 | (0,1) | 6 | FillBig→(0,5) dup · FillSmall→(3,1) D · EmptyBig→(0,0) dup · BigToSmall→(1,0) dup | 4 | 3 |
| 15 | (0,4) | 7 | FillBig→(0,5) dup · FillSmall→(3,4) dup · EmptyBig→(0,0) dup · BigToSmall→(3,1) dup | 4 | 4 |
| 16 | (3,1) | 7 | FillBig→(3,5) dup · EmptyBig→(3,0) dup · EmptySmall→(0,1) dup · SmallToBig→(0,4) dup | 4 | 4 |
| | | | **Total** | **58** | **43** |

**Reachable states (16), by depth.** These are listed in discovery order.

| Depth | States |
|------:|--------|
| 0 | (0,0) |
| 1 | (0,5), (3,0) |
| 2 | (3,5), (3,2), (0,3) |
| 3 | (0,2), (3,3) |
| 4 | (2,0), (1,5) |
| 5 | (2,5), (1,0) |
| 6 | (3,4), (0,1) |
| 7 | (0,4), (3,1) |

**Independent checks of the totals.**
- *States:* every action leaves at least one jug empty or full, and (0,0)
  has that property too. So reachable states are a subset of
  `{s ∈ {0,3}} ∪ {b ∈ {0,5}}`. That set has 12 + 4 = 16 members, and the
  table above reaches all of them.
- *Transitions:* the order does not matter, because each reachable state
  contributes its number of enabled actions. (0,0) has 2, (3,5) has 2, (0,5)
  and (3,0) have 3 each, and the other 12 have 4 each. 2 + 2 + 3 + 3 + 48 = 58.
- *Duplicates:* 58 − (16 − 1) = 43, from the statistics identity.
- *Dead ends* (`TerminalStates` in SEMANTICS.md §10, meaning states with an
  empty `Next`): every state has at least 2 enabled actions, so there are 0.
- *Max depth:* the BFS distance to (0,4) and (3,1) is 7. This does not depend
  on order.

### Expected results: unbounded runs

| Test | Config | Status | Discovered | Expanded | Transitions | Duplicates | Dead ends | Max depth |
|------|--------|--------|-----------:|---------:|------------:|-----------:|----------:|----------:|
| J1 | no invariant | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 7 |
| J2 | `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | 6 |

**J2 stopping point.** Rows 1–10 run in full (34 transitions, 23 duplicates).
Row 11, `(2,5)`, emits three duplicates and then `BigToSmall→(3,4)` as its
4th step. `(3,4)` is the 13th distinct state, and it violates `NotFour` at
discovery. That gives 34 + 4 = 38 transitions and 23 + 3 = 26 duplicates.
Identity check: 38 = (13 − 1) + 26. "Expanded" counts row 11, which was only
partly processed.

**J2 counterexample (6 steps):**

```
(0,0) -FillBig-> (0,5) -BigToSmall-> (3,2) -EmptySmall-> (0,2)
      -BigToSmall-> (2,0) -FillBig-> (2,5) -BigToSmall-> (3,4)
```

It is minimal. The only states with `b = 4` are `(3,4)` at depth 6 and
`(0,4)` at depth 7.

### Expected results: bounded runs (proposed D-012 semantics)

These rows assume the **Proposed** rules in [DECISIONS.md](DECISIONS.md)
D-012:
- states at the depth limit are expanded, but their new successors are
  counted as `CutOff` and not inserted;
- a state-limit run stops only when an (N+1)-th new state would be inserted.

They become binding only if D-012 is accepted. The identity here is
`transitions = (discovered − 1) + duplicates + cutoff`.

| Test | Config | Status | Disc. | Exp. | Tr | Dup | CutOff | Notes |
|------|--------|--------|------:|-----:|---:|----:|-------:|-------|
| J3 | depth 0 | `Bounded` | 1 | 1 | 2 | 0 | 2 | (0,5), (3,0) cut off |
| J4 | depth 5 | `Bounded` | 12 | 12 | 42 | 29 | 2 | (3,4), (0,1) cut off |
| J5 | depth 6 | `Bounded` | 14 | 14 | 50 | 35 | 2 | (0,4), (3,1) cut off |
| J6 | depth 7 (= max depth) | `Exhausted` | 16 | 16 | 58 | 43 | 0 | identical to J1 |
| J7 | depth 8 | `Exhausted` | 16 | 16 | 58 | 43 | 0 | identical to J1 |
| J8 | depth 5 + `NotFour` | `Bounded` | 12 | 12 | 42 | 29 | 2 | the violating (3,4) lies at depth 6 and is never checked |
| J9 | depth 6 + `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | identical to J2 |
| J10 | states 1 | `Bounded` | 1 | 1 | 1 | 0 | 1 | stops at the first new successor |
| J11 | states 15 | `Bounded` | 15 | 14 | 48 | 33 | 1 | the 16th state, (3,1), is cut off in row 14 |
| J12 | states 16 (= reachable) | `Exhausted` | 16 | 16 | 58 | 43 | 0 | identical to J1 |
| J13 | states 12 + `NotFour` | `Bounded` | 12 | 11 | 38 | 26 | 1 | (3,4) would be the 13th state, so it is not inserted or checked |
| J14 | states 13 + `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | identical to J2 |

J8 and J13 are the important negative cases. A violation exists just beyond
the limit, and the run correctly reports `Bounded`, not `Violation` or
`Exhausted`.

### Encoding sensitivity

If every action is always emitted, self-loops included (as in the classic
TLA+ `DieHard` spec), the distinct-state count stays at 16, but transitions
become 16 × 6 = 96 and duplicates become 81. Benchmark comparisons with other
tools must compare **distinct states**, and must state each tool's transition
convention ([BENCHMARKS.md §9](BENCHMARKS.md#9-comparisons-with-other-tools)).

### Provenance

Re-worked by hand on 2026-10-04 in the order above. Every row, total, and
bounded case was then cross-checked with a throwaway BFS script kept outside
the repository. The hand pass and the script agree on all values.
