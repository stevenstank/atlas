#!/usr/bin/env bash
# baseline.sh OUTDIR runs one Phase 3 baseline campaign (DECISIONS.md D-007,
# BENCHMARKS.md §5). Run it from the repository root of a clean checkout.
#
# Two independent sets, A then B, of RUNS rounds (default 10). Each round
# runs every workload of the shared table once, one atlasbench process per
# workload, profiles off, in an order shuffled for that round. The whole order
# comes from one master seed (SEED, default 64 random bits) through
# `atlasbench -plan`, and is written to plan.tsv, with each round's seed,
# before the first workload runs.
#
# OUTDIR must not exist. The campaign writes:
#   campaign.txt   provenance: times, master seed, rounds, git commit and
#                  dirty state, driver and its sha256, and the result
#   status         running, then complete, invalid, or aborted
#   workloads.txt  `atlasbench -list`
#   plan.tsv       every invocation: set, round, position, seed, workload
#   manifest.tsv   one line per invocation, appended as each one ends
#   loadavg.tsv    /proc/loadavg before each round and at the end
#   A.txt, B.txt   benchstat input: the output of every successful run
#   logs/          each invocation's stdout and stderr
#   failures.txt   only if the campaign failed
# An invocation fails if atlasbench exits nonzero (a wrong semantic result
# exits 1) or its stdout is not exactly one result line for the planned
# workload. The first failure is written to failures.txt, the status becomes
# invalid, and the campaign stops. Nothing is retried or skipped.
#
# The tree must be clean unless ALLOW_DIRTY=1, and atlasbench is built from
# it unless ATLASBENCH names a driver to use instead. Both overrides exist to
# test this script; both are recorded in campaign.txt.
set -euo pipefail

out=${1:?usage: baseline.sh OUTDIR}
runs=${RUNS:-10}
family=21 # D-007: the 21 wall-time comparisons of suite version 1

utc() { date -u +%Y-%m-%dT%H:%M:%S.%NZ; }

if [[ -e $out ]]; then
	echo "baseline.sh: $out exists; each campaign needs a new directory" >&2
	exit 2
fi
if ! [[ $runs =~ ^[1-9][0-9]*$ ]]; then
	echo "baseline.sh: RUNS must be a positive integer, got $runs" >&2
	exit 2
fi
seed=${SEED:-$(od -An -N8 -tu8 /dev/urandom | tr -d ' ')}
if ! [[ $seed =~ ^[0-9]+$ ]]; then
	echo "baseline.sh: SEED must be a decimal integer, got $seed" >&2
	exit 2
fi
commit=$(git rev-parse HEAD 2>/dev/null) || commit=unknown
porcelain=$(git --no-optional-locks status --porcelain 2>/dev/null) || porcelain="git status failed"
dirty=false
[[ -z $porcelain ]] || dirty=true
if [[ $dirty == true && ${ALLOW_DIRTY:-} != 1 ]]; then
	echo "baseline.sh: the working tree is not clean (or not a git checkout); commit first" >&2
	exit 2
fi
if [[ -z ${ATLASBENCH:-} && ! -f internal/bench/baseline.sh ]]; then
	echo "baseline.sh: run from the repository root" >&2
	exit 2
fi

mkdir -p "$out/logs"
echo running >"$out/status"
trap 'if [[ $(cat "$out/status") == running ]]; then echo aborted >"$out/status"; fi' EXIT
{
	echo "script: internal/bench/baseline.sh"
	echo "started-utc: $(utc)"
	echo "master-seed: $seed"
	echo "rounds-per-set: $runs"
	echo "sets: A B"
	echo "git-commit: $commit"
	echo "git-dirty: $dirty"
	echo "allow-dirty: ${ALLOW_DIRTY:-0}"
} >"$out/campaign.txt"
[[ $dirty == false ]] || printf '%s\n' "$porcelain" >"$out/git-status.txt"

fail() { # fail REASON [DETAIL...]: record the failure, mark the campaign invalid, stop
	local reason=$1
	shift
	{
		echo "time-utc: $(utc)"
		echo "reason: $reason"
		printf '%s\n' "$@"
	} >"$out/failures.txt"
	echo invalid >"$out/status"
	{
		echo "finished-utc: $(utc)"
		echo "result: invalid (failures.txt)"
	} >>"$out/campaign.txt"
	echo "baseline.sh: campaign invalid: $reason (see $out/failures.txt)" >&2
	exit 1
}

if [[ -n ${ATLASBENCH:-} ]]; then
	driver=$ATLASBENCH
	echo "driver: $driver (ATLASBENCH override, for testing only)" >>"$out/campaign.txt"
else
	driver=$out/atlasbench
	go build -o "$driver" ./internal/bench/cmd/atlasbench
	echo "driver: atlasbench built from the working tree" >>"$out/campaign.txt"
fi
echo "driver-sha256: $(sha256sum "$driver" | cut -d' ' -f1)" >>"$out/campaign.txt"
go version -m "$driver" >"$out/driver-buildinfo.txt" 2>&1 || true

"$driver" -list </dev/null >"$out/workloads.txt"
n=$(wc -l <"$out/workloads.txt")
((n == family)) || fail "atlasbench -list gave $n workloads; D-007's comparison family has $family"
"$driver" -plan -seed "$seed" -rounds "$runs" </dev/null >"$out/plan.tsv"
awk -F'\t' -v fam="$family" -v runs="$runs" '
	NR == FNR { known[$0] = 1; next }
	FNR == 1 { bad = bad || $0 != "set\tround\tposition\tseed\tworkload"; next }
	{
		k = $1 SUBSEP $2
		bad = bad || !($5 in known) || seen[k, $5]++ || ($1 != "A" && $1 != "B")
		count[k]++
		total++
	}
	END {
		bad = bad || total != 2 * runs * fam
		for (k in count) bad = bad || count[k] != fam
		exit bad
	}' "$out/workloads.txt" "$out/plan.tsv" || fail "plan.tsv is not $runs rounds of every workload in each set"

printf 'set\tround\tutc\tloadavg\n' >"$out/loadavg.tsv"
loadavg() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$(utc)" "$(cat /proc/loadavg 2>/dev/null || echo NA)" >>"$out/loadavg.tsv"; }
printf 'set\tround\tposition\tseed\tworkload\tstart-utc\tend-utc\texit-status\twall-time-ns\tstdout\tstderr\n' >"$out/manifest.tsv"
: >"$out/A.txt"
: >"$out/B.txt"

done_runs=0
prev=
while IFS=$'\t' read -r set round pos rseed w; do
	if [[ "$set $round" != "$prev" ]]; then
		loadavg "$set" "$round"
		prev="$set $round"
	fi
	tag=$(printf '%s-r%02d-p%02d' "$set" "$round" "$pos")
	so=logs/$tag.stdout
	se=logs/$tag.stderr
	start=$(utc)
	if "$driver" -bench "$w" -format bench </dev/null >"$out/$so" 2>"$out/$se"; then rc=0; else rc=$?; fi
	end=$(utc)
	wall=$(awk -v name="BenchmarkSuite/$w" '
		/^BenchmarkSuite\// { lines++; if ($1 == name && $2 == "1" && $3 ~ /^[0-9]+$/ && $4 == "ns/op") wall = $3 }
		END { if (lines == 1 && wall != "") print wall }' "$out/$so")
	printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$set" "$round" "$pos" "$rseed" "$w" \
		"$start" "$end" "$rc" "${wall:-NA}" "$so" "$se" >>"$out/manifest.tsv"
	if ((rc != 0)) || [[ -z $wall ]]; then
		why="exit status $rc"
		((rc != 0)) || why="no single result line for $w on stdout"
		fail "set $set, round $round, position $pos, $w: $why" \
			"set: $set" "round: $round" "position: $pos" "seed: $rseed" "workload: $w" \
			"exit-status: $rc" "start-utc: $start" "end-utc: $end" "stdout: $so" "stderr: $se" \
			"invocations-completed-before: $done_runs of $((2 * runs * family))" \
			"stderr follows:" "$(cat "$out/$se")"
	fi
	cat "$out/$so" >>"$out/$set.txt"
	done_runs=$((done_runs + 1))
done < <(tail -n +2 "$out/plan.tsv")

loadavg end -
echo complete >"$out/status"
{
	echo "finished-utc: $(utc)"
	echo "result: complete, $done_runs invocations; agreement is decided by the analysis in BENCHMARKS.md §5"
} >>"$out/campaign.txt"
echo "baseline.sh: wrote $out (A.txt, B.txt, manifest.tsv)"
