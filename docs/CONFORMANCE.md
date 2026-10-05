# Conformance models

> Status: hand-worked expectations, written before the engine existed.
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
| **Admitted** (`StatesDiscovered` in SEMANTICS.md §10) | Distinct states admitted to the visited set, including the initial state. Every admitted state is checked against the invariants. |
| **Expanded** | States for which `Next` was called, including a state whose expansion was stopped partway by a violation or a state limit. |
| **Init emissions / InitAdmitted / InitDuplicates** | *(D-012)* Initial-state emissions examined, distinct initial states admitted, and examined emissions of an already admitted initial state. Each water-jug and Grid2 run has 1 / 1 / 0. |
| **Transitions** | Every `(action, successor)` pair emitted by `Next` and examined by the engine. Emissions after a terminal condition are not examined and not counted. Only state-changing actions are emitted, so every transition changes the state. |
| **Duplicates** | Transitions whose successor was already in the visited set. |
| **CutoffTransitions** | *(D-012)* Transitions from a depth-D state to a previously unseen state at depth D+1, which is not admitted. Counted per transition, not per omitted state, so two transitions to the same omitted state count twice. Always 0 without a depth limit. |
| **StateLimitRefusals** | *(D-012)* 1 if the run ended because a previously unseen state, within the depth limit, would have exceeded the state limit N; otherwise 0. That state is not admitted or checked. The phase (initialization or expansion) is recorded. |
| **Dead ends** | Expanded states whose `Next` emitted nothing (`TerminalStates` in SEMANTICS.md §10). |
| **Max depth** | The largest BFS depth among admitted states. A state's BFS depth is the length of its shortest path from the initial state. |

**Identities and when they hold** (D-012 I1–I4):
- **I2, every run without an undetected callback-contract violation**
  (D-012 counting rule). `Transitions = (Admitted − InitAdmitted) +
  Duplicates + CutoffTransitions + R_exp`. A transition is counted only when
  it is assigned to exactly one branch: newly admitted, duplicate, depth
  cutoff, or expansion-phase state-limit refusal. Here `InitAdmitted = 1` and `R_exp =
  StateLimitRefusals`, because no refusal can happen during initialization
  with a single initial state and N ≥ 1. This is checked on every row below.
- **I3, `Exhausted` runs only.** Admitted = the number of reachable states,
  and Transitions = the number of emitted `(action, successor)` pairs over
  all reachable states. Every pair counts, even when two actions lead to the
  same state. For water jugs, Admitted = 16 and Transitions = 58. For Grid2,
  9 and 12.
- **I4, normal completion only** (`Exhausted`, or `Bounded` with
  `DepthLimit`). Transitions equals the sum of successor counts over admitted
  states. It does **not** hold for J2 (stopped mid-expansion by a violation),
  J11 (by a refusal), or J19 (interrupted).

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

| Test | Config | Status | Admitted | Expanded | Transitions | Duplicates | Dead ends | Max depth |
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

### Expected results: bounded runs (D-012 semantics)

> **D-012 (accepted 2026-10-04).** These rows are binding. They follow
> [DECISIONS.md](DECISIONS.md) D-012, which supersedes SEMANTICS.md §8 rules
> 2–3. In summary: depth-D states are checked and expanded, and an unseen successor at
> depth D+1 is refused as a `CutoffTransition`. The state limit N includes the
> initial state, and only an unseen state that would exceed N ends the run.
> For each successor, the checks run in this order: duplicate, then depth
> limit, then state limit, then admit and check the invariants. A depth
> cutoff is not terminal. If the run is interrupted before the frontier
> empties, the result is `Incomplete` even after cutoffs (J19).

Both models start at `(0,0)`. `—` means no limit. J19 and J20 need a
deterministic interruption: the test checks for interruption before every
dequeue (check interval K = 1, ARCHITECTURE.md §3.9) and requests
cancellation from a test hook after the stated number of expansions. Columns: Adm. = Admitted,
Exp. = Expanded, Tr = Transitions, Dup = Duplicates, Cut =
CutoffTransitions, SLR = StateLimitRefusals.

**Water jugs** (model above). Unbounded maximum depth 7, 16 reachable states.

| Test | D | N | Invariant | Expected outcome | Adm. | Exp. | Tr | Dup | Cut | SLR | Notes |
|------|--:|--:|-----------|------------------|-----:|-----:|---:|----:|----:|----:|-------|
| J3 | 0 | — | none | `Bounded` (depth) | 1 | 1 | 2 | 0 | 2 | 0 | (5,0) and (0,3) omitted |
| J4 | 5 | — | none | `Bounded` (depth) | 12 | 12 | 42 | 29 | 2 | 0 | (4,3) and (1,0) omitted |
| J5 | 6 | — | none | `Bounded` (depth) | 14 | 14 | 50 | 35 | 2 | 0 | (4,0) and (1,3) omitted |
| J6 | 7 | — | none | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 0 | depth-7 states (4,0), (1,3) are expanded; every successor already admitted |
| J7 | 8 | — | none | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 0 | limit above the maximum depth |
| J8 | 5 | — | `NotFour` | `Bounded` (depth) | 12 | 12 | 42 | 29 | 2 | 0 | violating (4,3) is at depth 6, omitted, **not reported** |
| J9 | 6 | — | `NotFour` | `Violation` at (4,3) | 13 | 11 | 38 | 26 | 0 | 0 | same as J2 |
| J10 | — | 1 | none | `Bounded` (state) | 1 | 1 | 1 | 0 | 0 | 1 | first unseen successor (5,0) refused |
| J11 | — | 15 | none | `Bounded` (state) | 15 | 14 | 48 | 33 | 0 | 1 | 16th state (1,3) refused while expanding (0,1) |
| J12 | — | 16 | none | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 0 | exactly N admitted; no further unseen state, so not `Bounded` |
| J13 | — | 12 | `NotFour` | `Bounded` (state) | 12 | 11 | 38 | 26 | 0 | 1 | violating (4,3) would be the 13th state; refused, **not reported** |
| J14 | — | 13 | `NotFour` | `Violation` at (4,3) | 13 | 11 | 38 | 26 | 0 | 0 | same as J2 |
| J15 | 6 | 14 | none | `Bounded` (depth) | 14 | 14 | 50 | 35 | 2 | 0 | both limits reached; the depth check comes first, so the D+1 states are depth cutoffs, not state-limit refusals |
| J16 | 6 | 13 | none | `Bounded` (state) | 13 | 12 | 42 | 29 | 0 | 1 | (1,0), at depth 6 within D, would be the 14th state |
| J17 | 7 | 16 | none | `Exhausted` | 16 | 16 | 58 | 43 | 0 | 0 | both limits equal the true size |
| J18 | 5 | 12 | `NotFour` | `Bounded` (depth) | 12 | 12 | 42 | 29 | 2 | 0 | violation beyond both limits, not reported |
| J19 | 5 | — | none | `Incomplete` (canceled) | 12 | 11 | 38 | 26 | 1 | 0 | cancellation requested after the 11th expansion. The cutoff (5,2)→(4,3) has occurred, but (0,1) is still in the frontier, so this is not `Bounded` |
| J20 | 5 | — | none | `Bounded` (depth) | 12 | 12 | 42 | 29 | 2 | 0 | cancellation requested after the 12th expansion. The frontier is already empty, so the run completed normally; same as J4 |

**Grid2** ([SEMANTICS.md §6](SEMANTICS.md#6-search-order-and-the-shortest-counterexample-guarantee)).
No invariant, unbounded maximum depth 4, 9 reachable states, 1 dead end
`(2,2)`.

| Test | D | N | Expected outcome | Adm. | Exp. | Tr | Dup | Cut | SLR | Notes |
|------|--:|--:|------------------|-----:|-----:|---:|----:|----:|----:|-------|
| G2 | 3 | — | `Bounded` (depth) | 8 | 8 | 12 | 3 | 2 | 0 | (2,1)→(2,2) and (1,2)→(2,2): one omitted state, two cutoff transitions |
| G3 | 4 | — | `Exhausted` | 9 | 9 | 12 | 4 | 0 | 0 | (2,2) is expanded and has no successors |
| G4 | — | 9 | `Exhausted` | 9 | 9 | 12 | 4 | 0 | 0 | N equals the reachable count |
| G5 | — | 8 | `Bounded` (state) | 8 | 7 | 11 | 3 | 0 | 1 | (2,2) refused while expanding (2,1) |
| G6 | 3 | 8 | `Bounded` (depth) | 8 | 8 | 12 | 3 | 2 | 0 | both limits reached; the depth check comes first |

(G1, the unbounded Grid2 run, is the exhaustive example in SEMANTICS.md §6:
9 / 9 / 12 / 4.)

**Grid2-MultiInit** (multiple initial states). This uses Grid2's
transitions and the invariant `Sum: x + y < 4`. `Init` emits, in order:
`(0,0)`, `(1,0)`, `(0,0)`, `(0,1)`, `(2,2)`. The third emission is a
duplicate. All initial states have depth 0. The reachable set is all 9 Grid2
states, with maximum depth 2 and 1 dead end, `(2,2)`. Extra columns: IE =
Init emissions examined, IA = InitAdmitted, ID = InitDuplicates, Phase = the
phase of the state-limit refusal.

| Test | D | N | Invariant | Expected outcome | IE | IA | ID | Adm. | Exp. | Tr | Dup | Cut | SLR (phase) | Notes |
|------|--:|--:|-----------|------------------|---:|---:|---:|-----:|-----:|---:|----:|----:|-------------|-------|
| M1 | — | — | none | `Exhausted` | 5 | 4 | 1 | 9 | 9 | 12 | 7 | 0 | 0 | I3 holds: 9 states, 12 edges |
| M2 | — | — | `Sum` | `Violation` at initial (2,2), 0-step trace | 5 | 4 | 1 | 4 | 0 | 0 | 0 | 0 | 0 | violation found during initialization |
| M3 | — | 2 | none | `Bounded` (state) | 4 | 2 | 1 | 2 | 0 | 0 | 0 | 0 | 1 (init) | (0,1) refused; 5th emission (2,2) never examined; initial set incomplete |
| M4 | — | 2 | `Sum` | `Bounded` (state) | 4 | 2 | 1 | 2 | 0 | 0 | 0 | 0 | 1 (init) | violating (2,2) never examined, **not reported** |
| M5 | — | 3 | none | `Bounded` (state) | 5 | 3 | 1 | 3 | 0 | 0 | 0 | 0 | 1 (init) | (2,2) refused as the 4th distinct initial state |
| M6 | — | 4 | none | `Bounded` (state) | 5 | 4 | 1 | 4 | 2 | 3 | 2 | 0 | 1 (expansion) | all initial states admitted; (2,0) refused while expanding (1,0) |
| M7 | 0 | — | none | `Bounded` (depth) | 5 | 4 | 1 | 4 | 4 | 6 | 2 | 4 | 0 | only depth-0 states admitted; (1,1) is cut off twice |
| M8 | — | 9 | none | `Exhausted` | 5 | 4 | 1 | 9 | 9 | 12 | 7 | 0 | 0 | N = reachable count with multiple initial states; same as M1 |
| M9 | — | 4 | `Sum` | `Violation` at initial (2,2) | 5 | 4 | 1 | 4 | 0 | 0 | 0 | 0 | 0 | exactly N admitted, then the violation; no refusal |

I1 (`IE = IA + ID + R_init`) and I2 hold on every M row. In M3, for example,
4 = 2 + 1 + 1. The refused initial emission is accounted for by
`StateLimitRefusals` in the initialization phase, and the unexamined fifth
emission appears nowhere.

**Empty initialization** (D-012 Initialization rule 8). Model `Empty`:
`Init` emits nothing, `Next` is never called, and the invariant is
`Never: false`, which fails on every state. A check before `Init` is
assumed not to fire.

| Test | D | N | Invariant | Expected outcome | IE | IA | ID | Adm. | Exp. | Tr | Dup | Cut | SLR | Notes |
|------|--:|--:|-----------|------------------|---:|---:|---:|-----:|-----:|---:|----:|----:|----:|-------|
| E1 | — | — | `Never` | `ModelError` (no initial states) | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | not `Exhausted`; no violation reported, because no state was admitted |
| E2 | 0 | 1 | `Never` | `ModelError` (no initial states) | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | limits are irrelevant; not `Bounded` |

I1 and I2 hold trivially (0 = 0). I3 and I4 do not apply, because the run did
not end normally. Max depth is undefined (no admitted state) and is reported
as absent, not 0.

**Required boundary cases** (TESTING.md §7):

| Case | Tests |
|------|-------|
| Depth limit where every boundary successor is already admitted ⇒ `Exhausted` | J6, G3 |
| Depth limit with an unseen successor beyond it ⇒ `Bounded` | J3, J4, J5, G2 |
| State limit exactly equal to the reachable count ⇒ `Exhausted` | J12, G4 |
| State limit that excludes a reachable state ⇒ `Bounded` | J10, J11, G5 |
| Violation only beyond a bound ⇒ `Bounded`, not reported as found | J8, J13, J18 |
| Violation within the bounds ⇒ `Violation` | J9, J14 |
| Both limits at once: check order | J15, J16, J17, G6 |
| Interrupted after a depth cutoff ⇒ `Incomplete`, not `Bounded` | J19 |
| Interruption requested once the frontier is empty ⇒ normal completion | J20 |
| Multiple initial states, duplicates, violation at initialization | M1, M2, M9 |
| State limit reached during initialization ⇒ `Bounded`, incomplete initial set | M3, M4, M5 |
| State limit equal to the reachable count with multiple initial states | M8 |
| Empty initial set ⇒ `ModelError`, not `Exhausted` or `Bounded` | E1, E2 |

> **Revision note.** In the previous version, J10, J11, and J13 showed
> `CutOff = 1`. D-012 now defines `CutoffTransitions` as depth-limit refusals
> only, so the state-limit refusal is counted separately as
> `StateLimitRefusals = 1`. The totals did not change.

### Encoding sensitivity

If every action is always emitted, self-loops included (as in the classic
TLA+ `DieHard` spec), the distinct-state count stays at 16, but transitions
become 16 × 6 = 96 and duplicates become 81. Benchmark comparisons with other
tools must compare **distinct states**, and must state each tool's transition
convention ([BENCHMARKS.md §9](BENCHMARKS.md#9-comparisons-with-other-tools)).

### Provenance

Re-worked by hand on 2026-10-04 in the order above. Every row, total, and
bounded case was then cross-checked with a throwaway BFS script kept outside
the repository. For bounded runs, the script implements the D-012 check order
exactly. The hand pass and the script agree on all values.

## Concurrent register (Phase 2)

Model: `models.Register` (its doc comment defines the state, actions, emit
order, and the `NoStaleRead` invariant). Values below are for the correct
variant, with no limits.

**Hand count, 2 clients × 1 operation.** Write each client's phase as I
(idle, 1 op left), R (reading), W (writing), or D (done). Classes `(A, B)`:

| Class | States | Why |
|-------|-------:|-----|
| (I,I) | 1 | initial |
| (R,I) (W,I) (I,R) (I,W) | 4 | nothing has completed yet |
| (R,R) (R,W) (W,R) (W,W) | 4 | same |
| (D,I) (I,D) | 2 + 2 | A's finished op was a read or a write |
| (D,R) (R,D) | 3 + 3 | after a write, B's pending read started before or after it finished (different recorded overwritten set) |
| (D,W) (W,D) | 3 + 3 | after a write, B's write started before or after it (different recorded completed set) |
| (D,D) | 9 | RR 1; WR and RW 2 each (the read hit the old value before the write, or missed after it); WW 4 (who finished first × overwritten set) |

Total 34 states, 9 of them terminal (the (D,D) class). Out-degree is 2 per
idle client with an op left and 1 per busy client: 4 + 4·3 + 4·2 + 4·2 +
6·1 + 6·1 = 44 transitions. Max depth 4. The engine and the reference
explorer agree.

**Larger sizes** (checked against the reference explorer, not by hand):
2×2: 449 states, 708 transitions, 61 terminal; 3×1: 325, 534, 49;
2×3: 3,674, 6,444, 309; 3×2: 25,543, 58,698, 901.

**Broken variant** (`NoInvalidate`): `Violation` of `NoStaleRead` with a
4-step trace at every size tested (a write must finish and a read must then
start and finish, so 4 is the minimum). It stops after 28 (2×1), 52 (2×2),
and 130 (3×2) admitted states, agreeing with the reference explorer.

## Alternating-bit protocol (Phase 2)

Model: `models.ABP` (its doc comment defines the state, actions, emit order,
and the `InOrderDelivery` and `AckedDelivered` invariants). Values below are
for the correct variant, with no limits.

**Hand count, 1 message, capacity 1.** Only m0 (bit 0) and ack 0 exist.
Write a state as `(acked, delivered, data, acks)`:

| # | State | Enabled steps | Out |
|---|-------|---------------|----:|
| 1 | (0, 0, [], []) | send | 1 |
| 2 | (0, 0, [m0], []) | lose m0, receive (deliver) | 2 |
| 3 | (0, 1, [], []) | send | 1 |
| 4 | (0, 1, [m0], []) | lose m0, receive (discard, re-ack) | 2 |
| 5 | (0, 1, [], [0]) | send, lose ack, receive ack (advance) | 3 |
| 6 | (0, 1, [m0], [0]) | lose m0, lose ack, receive ack (R blocked: ack channel full) | 3 |
| 7 | (1, 1, [], []) | none (terminal) | 0 |
| 8 | (1, 1, [m0], []) | lose m0, receive (discard, re-ack) | 2 |
| 9 | (1, 1, [], [0]) | lose ack, receive ack (ignored: done) | 2 |

Duplication never fires at capacity 1. `acked = 0, delivered = 0` with an
ack is impossible (acks come only from a receive), and (1, 1, [m0], [0]) is
unreachable: after S advances, a new ack needs R to consume the only m0, and
S no longer sends. Total 9 states, 16 transitions, 1 terminal; the deepest
state is #9 (send, receive, send, receive ack, receive), depth 5. The engine
and the reference explorer agree.

**Larger sizes** (messages × capacity; checked against the reference
explorer, not by hand; always 1 terminal state): 2×1: 18 states, 36
transitions, depth 8; 2×2: 51, 184, 10; 3×2: 81, 299, 13; 3×3: 172, 775,
15; 4×3: 240, 1,090, 18.

**Broken variant** (`IgnoreAckBit`): `Violation` of `AckedDelivered`. The
shortest trace is 5 steps when the ack channel can hold a duplicate
(capacity ≥ 2: the channel duplicates ack 0) and 6 steps at capacity 1 (a
retransmitted m0 makes R re-send ack 0). It stops after 13 (2×1), 22 (2×2),
and 22 (3×2) admitted states, agreeing with the reference explorer.
`InOrderDelivery` is not violated by either variant; a unit test checks its
monitor directly.

## Task queue (Phase 2)

Model: `models.TaskQueue` (its doc comment defines the state, actions, emit
order, and the `NoLostTask` and `AtMostOnce` invariants). Values below are
for the correct variant, with no limits.

**Hand count, 1 task, 1 worker.** Write a state as `(acked, runs, worker,
crashes left)`, with the worker idle (I), holding t0 (H), or having run it
(R). With no crashes the run is a chain: take, run, ack, giving 4 states, 3
transitions, 1 terminal, depth 3. With 1 crash:

| # | State | Steps | Out |
|---|-------|-------|----:|
| 1 | (no, 0, I, 1) | take | 1 |
| 2 | (no, 0, H, 1) | run, crash → #5 | 2 |
| 3 | (no, 1, R, 1) | ack, crash → #6 | 2 |
| 4 | (yes, 1, I, 1) | terminal | 0 |
| 5 | (no, 0, I, 0) | take | 1 |
| 6 | (no, 1, I, 0) | take → #10 | 1 |
| 7 | (no, 0, H, 0) | run → #8 | 1 |
| 8 | (no, 1, R, 0) | ack | 1 |
| 9 | (yes, 1, I, 0) | terminal | 0 |
| 10 | (no, 1, H, 0) | run (store skips it) → #8 | 1 |

10 states, 10 transitions, 2 terminal; the deepest is #9 (take, crash, take,
run, ack), depth 5. The engine and the reference explorer agree.

**Larger sizes** (tasks × workers × crashes; checked against the reference
explorer, not by hand; Crashes+1 terminal states): 2×2×1: 84 states, 184
transitions, depth 8; 2×2×2: 147, 368, 10; 3×2×2: 702, 2,070, 13; 3×3×2:
1,836, 7,245, 13; 4×3×3: 15,768, 75,972, 18.

**Broken variants.** `AckOnDelivery`: `Violation` of `NoLostTask` in 2
steps (take, crash), stopping after 4 (1×1×1) and 7 (2×2×1) states.
`NonIdempotent`: `Violation` of `AtMostOnce` in 5 steps (take, run, crash,
take, run), stopping after 10 (1×1×1), 48 (2×2×1), and 218 (3×3×2) states.
All agree with the reference explorer. Neither bug shows without a crash.
