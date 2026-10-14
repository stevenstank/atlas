#!/usr/bin/env bash
# baseline.sh OUTDIR records a Phase 3 baseline (DECISIONS.md D-007,
# BENCHMARKS.md §5): two independent sets of RUNS (default 10) atlasbench
# invocations of every workload, one workload per process. Set A runs the
# workloads in suite order, set B in reverse order; each round runs every
# workload once. Results go to OUTDIR/A.txt and OUTDIR/B.txt in Go benchmark
# format. Any failed run, including a wrong semantic result, stops the script.
# Run from the repository root.
set -euo pipefail
out=${1:?usage: baseline.sh OUTDIR}
runs=${RUNS:-10}
mkdir -p "$out"
go build -o "$out/atlasbench" ./internal/bench/cmd/atlasbench
mapfile -t names < <("$out/atlasbench" -list)
reversed=()
for ((i = ${#names[@]} - 1; i >= 0; i--)); do reversed+=("${names[i]}"); done

measure() { # measure SET WORKLOAD...
	local set=$1
	shift
	: >"$out/$set.txt"
	for ((r = 1; r <= runs; r++)); do
		for n in "$@"; do
			if ! "$out/atlasbench" -bench "$n" -format bench >>"$out/$set.txt"; then
				echo "baseline.sh: set $set, round $r, $n failed" >&2
				exit 1
			fi
		done
	done
}
measure A "${names[@]}"
measure B "${reversed[@]}"
echo "baseline.sh: wrote $out/A.txt and $out/B.txt"
