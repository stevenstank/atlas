#!/usr/bin/env bash
# baseline.sh OUTDIR runs one Phase 3 baseline campaign (DECISIONS.md D-007,
# BENCHMARKS.md §5).
#
# Two independent sets, A then B, of RUNS rounds (default 10). Each round
# runs every workload of the shared table once, one atlasbench process per
# workload, profiles off, in an order shuffled for that round. The whole order
# comes from one master seed (SEED, default 64 random bits) through
# `atlasbench -plan`, and is written to plan.tsv, with each round's seed,
# before the first workload runs.
#
# Preflight, before anything is written: BASH_ENV and ENV must be unset or
# empty (bash has already run a BASH_ENV file before this script starts, and
# the script cannot undo that; it can only refuse), RUNS and SEED must be canonical
# decimal integers (SEED as bench.ParseSeed accepts it), the git checkout
# holding this script must be clean, and OUTDIR must lie outside its worktree
# (after resolving symlinks and ..) and must not exist. A refusal exits 2 and
# creates nothing. OUTDIR is then claimed with one mkdir, so two campaigns
# can never share it.
#
# The campaign writes to OUTDIR:
#   campaign.txt   provenance: times, master seed, rounds, git commit, driver
#                  and its build metadata check, and the result
#   status         running, then complete, invalid, or aborted
#   driver-buildinfo.txt  `go version -m` of the driver
#   workloads.txt  `atlasbench -list`
#   plan.tsv       every invocation: set, round, position, seed, workload
#   manifest.tsv   one line per invocation, appended as each one ends
#   loadavg.tsv    /proc/loadavg before each round and at the end
#   A.txt, B.txt   benchstat input: the output of every successful run
#   logs/          each setup step's stderr, each invocation's stdout and stderr
#   failures.txt   only if the campaign failed
#
# The driver is built from the checkout with plain `go build`, in an
# environment reduced to PATH and HOME plus GOENV=off, GOFLAGS=, GOWORK=off,
# and GOTOOLCHAIN=local (BENCHMARKS.md §5). Before any workload runs, its
# build metadata must name HEAD as vcs.revision, say vcs.modified=false, and
# show the default build configuration. A binary supplied with ATLASBENCH is
# refused: its build configuration cannot be established from its metadata.
#
# After OUTDIR is claimed, the first failure of a setup step (build, build
# metadata and provenance, -list, -plan, plan validation) or of a workload
# invocation is written to failures.txt with its logs, the campaign is marked
# invalid, and the script exits 1. An invocation fails if atlasbench exits
# nonzero (a wrong semantic result exits 1) or its stdout is not exactly one
# result line for the planned workload. Nothing is retried or skipped.
set -euo pipefail
export LC_ALL=C

family=21 # D-007: the 21 wall-time comparisons of suite version 1

refuse() {
	echo "baseline.sh: $*" >&2
	exit 2
}
utc() { date -u +%Y-%m-%dT%H:%M:%S.%NZ; }

[[ $# -eq 1 && -n $1 ]] || refuse "usage: baseline.sh OUTDIR"
for v in BASH_ENV ENV; do
	[[ -z ${!v:-} ]] || refuse "$v is set; unset it and rerun (this script never reads the file it names)"
done
runs=${RUNS-10}
[[ $runs =~ ^[1-9][0-9]{0,3}$ ]] || refuse "RUNS must be an integer from 1 to 9999, got '$runs'"
if [[ ${SEED+set} ]]; then
	seed=$SEED
else
	seed=$(od -An -N8 -tu8 /dev/urandom | tr -d ' \n')
fi
# The rule of bench.ParseSeed: the seed recorded is the seed -plan uses.
if ! [[ $seed =~ ^(0|[1-9][0-9]{0,19})$ ]] || [[ ${#seed} -eq 20 && $seed > 18446744073709551615 ]]; then
	refuse "SEED must be a decimal integer from 0 to 18446744073709551615 with no sign, prefix, spaces, or leading zero; got '$seed'"
fi

[[ -z ${ATLASBENCH+set} ]] || refuse "ATLASBENCH is not supported: a supplied binary's build cannot be verified" \
	"(go version -m does not record -overlay, -toolexec, or the toolchain); unset it, and the campaign builds its driver"
unset CDPATH
here=$(realpath -e -- "$(dirname -- "${BASH_SOURCE[0]}")")
top=$(git -C "$here" rev-parse --show-toplevel 2>/dev/null) || refuse "$here is not in a git checkout"
top=$(realpath -e -- "$top") || refuse "cannot resolve the worktree $top"
commit=$(git -C "$top" rev-parse HEAD 2>/dev/null) || refuse "cannot read HEAD in $top"
[[ $commit =~ ^[0-9a-f]{40}$|^[0-9a-f]{64}$ ]] || refuse "HEAD is not a commit hash: '$commit'"
porcelain=$(git -C "$top" --no-optional-locks status --porcelain 2>/dev/null) || refuse "git status failed in $top"
[[ -z $porcelain ]] || refuse "the working tree $top is not clean; commit first:"$'\n'"$porcelain"
out=$(realpath -m -- "$1") || refuse "cannot resolve $1"
if [[ $out == "$top" || $out == "$top"/* ]]; then
	refuse "OUTDIR $out is inside the worktree $top; campaign files there would mark the build dirty"
fi
[[ ! -e $out && ! -L $out ]] || refuse "$out exists; each campaign needs a new directory"
mkdir -p -- "$(dirname -- "$out")" || refuse "cannot create the parent directory of $out"
mkdir -- "$out" 2>/dev/null || refuse "cannot claim $out: it was created meanwhile (another campaign?) or cannot be made"

fail() { # fail REASON [DETAIL...]: record the failure, mark the campaign invalid, exit 1
	set +e
	local reason=$1 record
	shift
	record=$(printf 'time-utc: %s\nreason: %s\n' "$(utc)" "$reason" && printf '%s\n' "$@")
	if ! printf '%s\n' "$record" >"$out/failures.txt"; then
		printf 'baseline.sh: could not write %s/failures.txt; the record was:\n%s\n' "$out" "$record" >&2
	fi
	echo invalid >"$out/status" || echo "baseline.sh: could not mark $out invalid" >&2
	printf 'finished-utc: %s\nresult: invalid (failures.txt)\n' "$(utc)" >>"$out/campaign.txt" ||
		echo "baseline.sh: could not update $out/campaign.txt" >&2
	echo "baseline.sh: campaign invalid: $reason (see $out/failures.txt)" >&2
	exit 1
}

echo running >"$out/status" || fail "setup: cannot write $out/status"
trap 'if [[ $(cat "$out/status" 2>/dev/null) == running ]]; then echo aborted >"$out/status"; fi' EXIT
{
	echo "script: internal/bench/baseline.sh"
	echo "started-utc: $(utc)"
	echo "master-seed: $seed"
	echo "rounds-per-set: $runs"
	echo "sets: A B"
	echo "worktree: $top"
	echo "git-commit: $commit"
	echo "git-dirty: false"
} >"$out/campaign.txt" || fail "setup: cannot write campaign.txt"
mkdir -- "$out/logs" || fail "setup: cannot create $out/logs"

setup() { # setup PHASE STDOUT COMMAND...: run a setup step; record any failure
	local phase=$1 so=$2 se=logs/setup-$1.stderr rc=0
	shift 2
	"$@" </dev/null >"$out/$so" 2>"$out/$se" || rc=$?
	((rc == 0)) || fail "setup: $phase exited with status $rc" "phase: $phase" "command: $*" \
		"exit-status: $rc" "master-seed: $seed" "rounds-per-set: $runs" "stdout: $so" "stderr: $se" \
		"stderr follows:" "$(cat "$out/$se")"
}

driver=$out/atlasbench
# GOENV=off: with only GOFLAGS= the go command would still apply `go env -w` values.
build_env=(env -i "PATH=$PATH" "HOME=${HOME:-}" GOENV=off GOFLAGS= GOWORK=off GOTOOLCHAIN=local)
build() (cd "$top" && "${build_env[@]}" go build -o "$driver" ./internal/bench/cmd/atlasbench)
setup build logs/setup-build.stdout build
{
	echo "driver: $driver (built from $top)"
	echo "build-command: go build -o $driver ./internal/bench/cmd/atlasbench"
	echo "build-environment: ${build_env[*]:1}"
} >>"$out/campaign.txt"
setup buildinfo driver-buildinfo.txt go version -m "$driver"
vcs() { awk -v key="vcs.$1=" '$1 == "build" && index($2, key) == 1 { n++; v = substr($2, length(key) + 1) }
	END { if (n == 1) print v }' "$out/driver-buildinfo.txt"; }
rev=$(vcs revision)
mod=$(vcs modified)
{
	echo "driver-sha256: $(sha256sum "$driver" | cut -d' ' -f1)"
	echo "driver-vcs-revision: ${rev:-none}"
	echo "driver-vcs-modified: ${mod:-none}"
} >>"$out/campaign.txt"
[[ $rev == "$commit" ]] || fail "setup: provenance: the driver's vcs.revision is '${rev:-none}', HEAD is $commit" \
	"phase: provenance" "buildinfo: driver-buildinfo.txt"
[[ $mod == false ]] || fail "setup: provenance: the driver's vcs.modified is '${mod:-none}', not false" \
	"phase: provenance" "buildinfo: driver-buildinfo.txt"
echo "provenance: ok (vcs.revision is HEAD, vcs.modified=false)" >>"$out/campaign.txt"
# The recorded settings are informational, not exhaustive (cmd/go): they
# confirm the build above; the controlled build environment establishes it.
why=$(awk '
	function no(msg) { if (why == "") why = msg }
	$1 == "path" { paths++; if ($2 != "github.com/stevenstank/atlas/internal/bench/cmd/atlasbench") no("main package " $2) }
	$1 == "dep" { no("dependency " $2 "; the module has none") }
	$1 == "build" {
		k = $2; sub(/=.*/, "", k); v = substr($2, length(k) + 2)
		if (k ~ /^vcs(\.|$)/) next
		seen[k]++
		if (k == "-buildmode") { if (v != "exe") no($2) }
		else if (k == "-compiler") { if (v != "gc") no($2) }
		else if (k ~ /^CGO_(C|CPP|CXX|LD)FLAGS$/) { if (v != "" || NF > 2) no($2) }
		else if (k == "GOAMD64") { if (v != "v1") no($2) }
		else if (k !~ /^(CGO_ENABLED|GOARCH|GOOS|DefaultGODEBUG)$/) no("non-default build setting " $2)
	}
	END {
		if (paths != 1) no("no single main package path")
		else if (seen["-buildmode"] != 1 || seen["-compiler"] != 1) no("-buildmode or -compiler not recorded once")
		print why
	}' "$out/driver-buildinfo.txt")
{
	echo "driver-toolchain: $(head -n 1 "$out/driver-buildinfo.txt" | awk '{print $NF}')"
	echo "driver-build-settings: $(awk '$1 == "build" && $2 !~ /^vcs/ { sub(/^[ \t]*build[ \t]+/, ""); printf "%s%s", sep, $0; sep = "; " }' "$out/driver-buildinfo.txt")"
} >>"$out/campaign.txt"
[[ -z $why ]] || fail "setup: build configuration: $why" "phase: build configuration" "buildinfo: driver-buildinfo.txt"
echo "build-configuration: ok (no build flags, default settings, no dependencies)" >>"$out/campaign.txt"

setup list workloads.txt "$driver" -list
n=$(wc -l <"$out/workloads.txt")
((n == family)) || fail "setup: atlasbench -list gave $n workloads; D-007's comparison family has $family" \
	"phase: list" "stdout: workloads.txt"
setup plan plan.tsv "$driver" -plan -seed "$seed" -rounds "$runs"
# The plan must be exactly: set A rounds 1..RUNS, then set B rounds 1..RUNS,
# each with positions 1..21, every workload once, and one canonical seed per
# round that no other round uses.
why=$(awk -F'\t' -v fam="$family" -v runs="$runs" '
	function no(msg) { if (why == "") why = "plan.tsv line " FNR ": " msg }
	NR == FNR { if ($0 == "" || $0 in known) no("bad workload list"); known[$0] = 1; next }
	FNR == 1 { header = 1; if ($0 != "set\tround\tposition\tseed\tworkload") no("bad header"); next }
	{
		i = rows++
		per = runs * fam
		set = i < per ? "A" : "B"
		round = int((i % per) / fam) + 1
		pos = i % fam + 1
		if (NF != 5) no(NF " fields, want 5")
		else if ($1 != set || $2 != round "" || $3 != pos "") no($1 " " $2 " " $3 ", want set " set " round " round " position " pos)
		else if ($4 !~ /^(0|[1-9][0-9]*)$/ || length($4) > 20 || (length($4) == 20 && $4 > "18446744073709551615")) no("bad seed " $4)
		else if (!($5 in known)) no("unknown workload " $5)
		else if (seen[set, round, $5]++) no("workload " $5 " repeats in set " set " round " round)
		else if (pos == 1) { if ($4 in used) no("seed " $4 " reused"); used[$4] = 1; rs = $4 }
		else if ($4 != rs) no("seed " $4 " differs from the round seed " rs)
	}
	END {
		if (!header) why = "plan.tsv is empty"
		else if (why == "" && rows != 2 * runs * fam) why = "plan.tsv has " rows " rows, want " 2 * runs * fam
		print why
	}' "$out/workloads.txt" "$out/plan.tsv")
[[ -z $why ]] || fail "setup: $why" "phase: plan validation" "plan: plan.tsv"

printf 'set\tround\tutc\tloadavg\n' >"$out/loadavg.tsv"
loadavg() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$(utc)" "$(cat /proc/loadavg 2>/dev/null || echo NA)" >>"$out/loadavg.tsv"; }
printf 'set\tround\tposition\tseed\tworkload\tstart-utc\tend-utc\texit-status\twall-time-ns\tstdout\tstderr\n' >"$out/manifest.tsv"
: >"$out/A.txt"
: >"$out/B.txt"

total=$((2 * runs * family))
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
			"invocations-completed-before: $done_runs of $total" \
			"stderr follows:" "$(cat "$out/$se")"
	fi
	cat "$out/$so" >>"$out/$set.txt"
	done_runs=$((done_runs + 1))
done < <(tail -n +2 "$out/plan.tsv")
((done_runs == total)) || fail "only $done_runs of $total planned invocations ran"

loadavg end -
echo complete >"$out/status"
{
	echo "finished-utc: $(utc)"
	echo "result: complete, $done_runs invocations; agreement is decided by the analysis in BENCHMARKS.md §5"
} >>"$out/campaign.txt"
echo "baseline.sh: wrote $out (A.txt, B.txt, manifest.tsv)"
