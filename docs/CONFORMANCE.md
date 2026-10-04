# Conformance models

> Status: hand-worked expectations, written before any engine code exists.
> Phase 1 turns each section into a test. If the engine disagrees with this
> document, first re-check the arithmetic here, then fix whichever is wrong.

The first conformance model, `Grid2`, is in [SEMANTICS.md §6](SEMANTICS.md#6-search-order-and-the-shortest-counterexample-guarantee).
This document adds the water-jug model.

## Water jugs (big 5 L, small 3 L)

### Model definition

These conventions are fixed. Changing any of them changes the expected
numbers.

- **State:** `(B, S)`. `B` is the litres in the **big** jug (capacity 5),
  and `S` is the litres in the **small** jug (capacity 3). Both are integers.
  Every tuple in this document is written big jug first.
- **Key:** two bytes, `B` then `S`.
- **Initial states:** `[(0,0)]`.
- **Actions**, emitted in exactly this order. An action is emitted **only if
  it changes the state**, so there are no self-loops:

| # | Action       | Enabled when       | Result |
|---|--------------|--------------------|--------|
| 1 | `FillBig`    | `B < 5`            | `(5, S)` |
| 2 | `FillSmall`  | `S < 3`            | `(B, 3)` |
| 3 | `EmptyBig`   | `B > 0`            | `(0, S)` |
| 4 | `EmptySmall` | `S > 0`            | `(B, 0)` |
| 5 | `BigToSmall` | `B > 0 && S < 3`   | `a = min(B, 3−S)`; `(B−a, S+a)` |
| 6 | `SmallToBig` | `S > 0 && B < 5`   | `a = min(S, 5−B)`; `(B+a, S−a)` |

- **Invariant:** `NotFour: B != 4`. A state violates it when the big jug holds
  exactly 4 L.

> **Revision note (2026-10-04).** An earlier version of this document used the
> order FillSmall, FillBig, EmptySmall, EmptyBig, SmallToBig, BigToSmall. The
> owner has specified the order above. Totals for the exhaustive run (states,
> transitions, duplicates, max depth, dead ends) do not depend on order, and
> they were re-verified unchanged. The expansion table and the statistics at
> the point a violation stops the run *do* depend on order, and have changed.
> The earlier values were correct for the earlier order. The 6-step
> counterexample is the same under both orders.
>
> **Revision note (2026-10-04, later).** The tuple convention changed from
> `(s, b)` (small first) to the owner's `(B, S)` (big first). Every state was
> swapped mechanically, then the swapped table was compared row by row with a
> freshly generated `(B, S)` run, with no mismatches. No number changed.

### How each statistic is counted

These definitions refine SEMANTICS.md §10 for this document.

| Statistic | Counts |
|-----------|--------|
| **Discovered** | Distinct states admitted to the visited set, including the initial state. |
| **Expanded** | States for which `Next` was called, including a state whose expansion was stopped partway by a violation or a state limit. |
| **Transitions** | Every `(action, successor)` pair emitted by `Next` and examined by the engine. Only state-changing actions are emitted, so every transition changes the state. |
| **Duplicates** | Transitions whose successor was already in the visited set. |
| **CutoffTransitions** | Transitions whose successor was *not* in the visited set but was refused by a depth or state limit (D-012, proposed). These are counted per transition, not per distinct state. |
| **Dead ends** | Expanded states whose `Next` emitted nothing (`TerminalStates` in SEMANTICS.md §10). |
| **Max depth** | The largest BFS depth among discovered states. A state's BFS depth is the length of its shortest path from the initial state. |

Identity, checked on every row:
`Transitions = (Discovered − 1) + Duplicates + CutoffTransitions`.

### Full BFS expansion (no invariant)

`D` means a new state was discovered. `dup` means the target was already
visited. Rows are in expansion order, which equals discovery order.

| # | Expand | Depth | Steps emitted (in order) | Tr | Dup |
|--:|--------|------:|--------------------------|---:|----:|
| 1 | (0,0) | 0 | FillBig→(5,0) D · FillSmall→(0,3) D | 2 | 0 |
| 2 | (5,0) | 1 | FillSmall→(5,3) D · EmptyBig→(0,0) dup · BigToSmall→(2,3) D | 3 | 1 |
| 3 | (0,3) | 1 | FillBig→(5,3) dup · EmptySmall→(0,0) dup · SmallToBig→(3,0) D | 3 | 2 |
| 4 | (5,3) | 2 | EmptyBig→(0,3) dup · EmptySmall→(5,0) dup | 2 | 2 |
| 5 | (2,3) | 2 | FillBig→(5,3) dup · EmptyBig→(0,3) dup · EmptySmall→(2,0) D · SmallToBig→(5,0) dup | 4 | 3 |
| 6 | (3,0) | 2 | FillBig→(5,0) dup · FillSmall→(3,3) D · EmptyBig→(0,0) dup · BigToSmall→(0,3) dup | 4 | 3 |
| 7 | (2,0) | 3 | FillBig→(5,0) dup · FillSmall→(2,3) dup · EmptyBig→(0,0) dup · BigToSmall→(0,2) D | 4 | 3 |
| 8 | (3,3) | 3 | FillBig→(5,3) dup · EmptyBig→(0,3) dup · EmptySmall→(3,0) dup · SmallToBig→(5,1) D | 4 | 3 |
| 9 | (0,2) | 4 | FillBig→(5,2) D · FillSmall→(0,3) dup · EmptySmall→(0,0) dup · SmallToBig→(2,0) dup | 4 | 3 |
| 10 | (5,1) | 4 | FillSmall→(5,3) dup · EmptyBig→(0,1) D · EmptySmall→(5,0) dup · BigToSmall→(3,3) dup | 4 | 3 |
| 11 | (5,2) | 5 | FillSmall→(5,3) dup · EmptyBig→(0,2) dup · EmptySmall→(5,0) dup · BigToSmall→(4,3) D | 4 | 3 |
| 12 | (0,1) | 5 | FillBig→(5,1) dup · FillSmall→(0,3) dup · EmptySmall→(0,0) dup · SmallToBig→(1,0) D | 4 | 3 |
| 13 | (4,3) | 6 | FillBig→(5,3) dup · EmptyBig→(0,3) dup · EmptySmall→(4,0) D · SmallToBig→(5,2) dup | 4 | 3 |
| 14 | (1,0) | 6 | FillBig→(5,0) dup · FillSmall→(1,3) D · EmptyBig→(0,0) dup · BigToSmall→(0,1) dup | 4 | 3 |
| 15 | (4,0) | 7 | FillBig→(5,0) dup · FillSmall→(4,3) dup · EmptyBig→(0,0) dup · BigToSmall→(1,3) dup | 4 | 4 |
| 16 | (1,3) | 7 | FillBig→(5,3) dup · EmptyBig→(0,3) dup · EmptySmall→(1,0) dup · SmallToBig→(4,0) dup | 4 | 4 |
| | | | **Total** | **58** | **43** |

**Reachable states (16), by depth.** These are listed in discovery order.

| Depth | States |
|------:|--------|
| 0 | (0,0) |
| 1 | (5,0), (0,3) |
| 2 | (5,3), (2,3), (3,0) |
| 3 | (2,0), (3,3) |
| 4 | (0,2), (5,1) |
| 5 | (5,2), (0,1) |
| 6 | (4,3), (1,0) |
| 7 | (4,0), (1,3) |

**Independent checks of the totals.**
- *States:* every action leaves at least one jug empty or full, and (0,0)
  has that property too. So reachable states are a subset of
  `{B ∈ {0,5}} ∪ {S ∈ {0,3}}`. That set has 2×4 + 4×2 = 16 members, and the
  table above reaches all of them.
- *Transitions:* the order does not matter, because each reachable state
  contributes its number of enabled actions. (0,0) has 2, (5,3) has 2, (5,0)
  and (0,3) have 3 each, and the other 12 have 4 each. 2 + 2 + 3 + 3 + 48 = 58.
- *Duplicates:* 58 − (16 − 1) = 43, from the statistics identity.
- *Dead ends* (`TerminalStates` in SEMANTICS.md §10, meaning states with an
  empty `Next`): every state has at least 2 enabled actions, so there are 0.
- *Max depth:* the BFS distance to (4,0) and (1,3) is 7. This does not depend
  on order.

### Expected results: unbounded runs

| Test | Config | Status | Discovered | Expanded | Transitions | Duplicates | Dead ends | Max depth |
|------|--------|--------|-----------:|---------:|------------:|-----------:|----------:|----------:|
| J1 | no invariant | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 7 |
| J2 | `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | 6 |

**J2 stopping point.** Rows 1–10 run in full (34 transitions, 23 duplicates).
Row 11, `(5,2)`, emits three duplicates and then `BigToSmall→(4,3)` as its
4th step. `(4,3)` is the 13th distinct state, and it violates `NotFour` at
discovery. That gives 34 + 4 = 38 transitions and 23 + 3 = 26 duplicates.
Identity check: 38 = (13 − 1) + 26. "Expanded" counts row 11, which was only
partly processed.

**J2 counterexample (6 steps):**

```
(0,0) -FillBig-> (5,0) -BigToSmall-> (2,3) -EmptySmall-> (2,0)
      -BigToSmall-> (0,2) -FillBig-> (5,2) -BigToSmall-> (4,3)
```

It is minimal. The only states with `B = 4` are `(4,3)` at depth 6 and
`(4,0)` at depth 7.

### Expected results: bounded runs (proposed D-012 semantics)

These rows assume the **Proposed** rules in [DECISIONS.md](DECISIONS.md)
D-012, and they become binding only if D-012 is accepted:
- the depth limit D is the maximum depth of states that may be admitted and
  checked. States at depth D are expanded. A previously unseen successor is
  not admitted and counts as one `CutoffTransition`;
- the state limit N counts the initial state. A previously unseen state that
  would make the count N + 1 is not admitted or checked, and the run returns
  `Bounded` immediately.

Columns: Disc. = Discovered, Exp. = Expanded, Tr = Transitions,
Dup = Duplicates, Cut = CutoffTransitions.

| Test | Config | Status | Disc. | Exp. | Tr | Dup | Cut | Notes |
|------|--------|--------|------:|-----:|---:|----:|----:|-------|
| J3 | depth 0 | `Bounded` | 1 | 1 | 2 | 0 | 2 | (5,0), (0,3) refused |
| J4 | depth 5 | `Bounded` | 12 | 12 | 42 | 29 | 2 | (4,3), (1,0) refused |
| J5 | depth 6 | `Bounded` | 14 | 14 | 50 | 35 | 2 | (4,0), (1,3) refused |
| J6 | depth 7 (= max depth) | `Exhausted` | 16 | 16 | 58 | 43 | 0 | every successor of (4,0) and (1,3) already discovered; same as J1 |
| J7 | depth 8 | `Exhausted` | 16 | 16 | 58 | 43 | 0 | same as J1 |
| J8 | depth 5 + `NotFour` | `Bounded` | 12 | 12 | 42 | 29 | 2 | violating (4,3) is at depth 6, refused, never checked |
| J9 | depth 6 + `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | same as J2 |
| J10 | states 1 | `Bounded` | 1 | 1 | 1 | 0 | 1 | first new successor (5,0) refused |
| J11 | states 15 | `Bounded` | 15 | 14 | 48 | 33 | 1 | 16th state (1,3) refused in row 14 |
| J12 | states 16 (= reachable) | `Exhausted` | 16 | 16 | 58 | 43 | 0 | reaching exactly N is not `Bounded`; same as J1 |
| J13 | states 12 + `NotFour` | `Bounded` | 12 | 11 | 38 | 26 | 1 | (4,3) would be the 13th state, so it is refused and never checked |
| J14 | states 13 + `NotFour` | `Violation` | 13 | 11 | 38 | 26 | 0 | same as J2 |

**Required boundary cases** (TESTING.md §7):

| Case | Water jugs | Grid2 ([DECISIONS.md](DECISIONS.md) D-012) |
|------|------------|-------|
| Depth limit where every successor is already discovered ⇒ `Exhausted` | J6 | depth 4 |
| Depth limit with an unseen successor beyond it ⇒ `Bounded` | J3, J4, J5 | depth 3 |
| State limit exactly equal to the reachable count ⇒ `Exhausted` | J12 | states 9 |
| State limit that excludes another reachable state ⇒ `Bounded` | J10, J11 | states 8 |
| Violation only beyond a bound ⇒ `Bounded`, not reported as found | J8, J13 | — |
| Violation within the bound ⇒ `Violation` | J9, J14 | — |

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
