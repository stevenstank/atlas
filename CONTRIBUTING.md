# Contributing to Atlas

Atlas is at the specification stage. Read [README.md](README.md),
[docs/SEMANTICS.md](docs/SEMANTICS.md), and [docs/ROADMAP.md](docs/ROADMAP.md)
before proposing changes.

> **Never commit or push on behalf of the project owner.**
>
> This applies to human contributors working in the owner's checkout and to
> every automated assistant, agent, bot, script, hook, or CI workflow. Do not
> commit, amend, rebase, cherry-pick, or push. Leave changes uncommitted and
> report what you changed. The owner reviews and commits all history. If about
> 700 lines of uncommitted changes have built up, pause and ask the owner to
> commit before you continue.

## Workflow

1. **Find the phase.** Work must belong to the current phase in
   [docs/ROADMAP.md](docs/ROADMAP.md). Work from a later phase needs the
   owner's approval first.
2. **Check the decisions.** If the change touches an open or decided item in
   [docs/DECISIONS.md](docs/DECISIONS.md), update that record or reference it.
3. **Write the test first** whenever you can. For semantic behavior, write the
   expected outcome down before writing the code.
4. **Implement** the smallest change that satisfies the test.
5. **Verify** with the checks listed below.
6. **Report**: list the files changed, the commands run, the results, and any
   follow-up work.

## Coding standards

- Go version: the version pinned in `go.mod` once it exists (development
  currently uses Go 1.26).
- Format with `gofmt`. `go vet ./...` must be clean.
- Prefer the standard library. A new dependency needs a written justification
  in the change description: what it provides, what the alternative costs, and
  a measurement where performance is the reason.
- Exported identifiers need doc comments that state the guarantees and
  preconditions, not only what the identifier does.
- Return errors for expected failures. Panics are for violated internal
  invariants (programmer error) only.
- Do not add abstractions for hypothetical future needs. Add the interface when
  the second implementation shows up.
- Do not mix refactoring with behavior changes in the same change.

## Test requirements

See [docs/TESTING.md](docs/TESTING.md) for the full strategy. At a minimum:

- `go test ./...` passes.
- `go test -race ./...` passes for any change that touches concurrency.
- New search or deduplication behavior has a test against the reference
  (naive) explorer on at least one model with a known state count.
- Every bug fix includes a regression test that fails without the fix.
- Tests check semantic outcomes: state counts, verdicts, traces, and
  completion status. A line being executed is not enough.

## Benchmark requirements

See [docs/BENCHMARKS.md](docs/BENCHMARKS.md).

- A change that claims to improve performance must include before and after
  results from the fixed benchmark suite on the same machine. Report the
  median of repeated runs, measured with `benchstat` or an equivalent
  statistical comparison.
- Every benchmark run must also confirm the expected semantic result (state
  count, verdict). A faster run that produces a different answer is a bug, not
  an improvement.
- Record the hardware, OS, Go version, build flags, and the exact command.
- Never state a comparison with another tool without equivalent workloads,
  verified equivalent results, and a published method.

## Review expectations

Reviewers check, in this order:

1. **Semantics.** Does the change preserve every guarantee in
   [docs/SEMANTICS.md](docs/SEMANTICS.md)? Can an incomplete run be mistaken
   for an exhaustive one?
2. **Tests.** Do the tests check meaning, and would they fail if the change
   were wrong?
3. **Evidence.** Are performance claims backed by reproducible numbers?
4. **Scope.** Does the change do only what it claims?
5. **Simplicity.** Is any new complexity justified by a measured need?

Changes to the public API or to documented semantics must update the
documentation in the same change.

## Reporting bugs

Include the model (or a minimized version of it), the configuration, the
expected result, the actual result, and the Go version and platform. A
counterexample that fails to replay or a wrong state count is a critical bug.
