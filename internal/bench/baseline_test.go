package bench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/atlas/internal/bench"
)

// stubDriver stands in for atlasbench in baseline.sh tests, so that the
// script's bookkeeping is tested without measuring anything. -list and -plan
// print what bench.Workloads and bench.Plan give (the real driver's -plan is
// tested in cmd/atlasbench). -bench appends the workload to calls, one line
// per process, and prints a fake result line. It exits 9 on any argument
// list the script must not use, such as a profile flag.
const stubDriver = `#!/usr/bin/env bash
case "$1" in
-list) cat "$STUB/list" ;;
-plan) [[ "$*" == "-plan -seed $STUB_SEED -rounds $STUB_ROUNDS" ]] || exit 9; cat "$STUB/plan" ;;
-bench)
	[[ $# -eq 4 && $3 == -format && $4 == bench ]] || exit 9
	echo "$2" >>"$STUB/calls"
	echo "stub stderr for $2" >&2
	[[ $2 == "${STUB_FAIL:-}" ]] && { echo "atlasbench: $2: bench: wrong result" >&2; exit 1; }
	[[ $2 == "${STUB_GARBLE:-}" ]] && { echo "BenchmarkSuite/other 1 5 ns/op"; exit 0; }
	echo "goos: linux"
	echo "BenchmarkSuite/$2 1 1000 ns/op 4000000 peak-rss-bytes"
	;;
*) exit 9 ;;
esac
`

type campaign struct {
	dir, out string
	plan     []bench.PlanEntry
	code     int
	stderr   string
}

// runBaseline runs baseline.sh with the stub driver from an empty directory
// outside any git checkout, so the tree counts as dirty.
func runBaseline(t *testing.T, rounds int, seed uint64, env ...string) campaign {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("baseline.sh needs GNU date and /proc")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	script, err := filepath.Abs("baseline.sh")
	if err != nil {
		t.Fatal(err)
	}
	stub := t.TempDir()
	var names []string
	for _, w := range bench.Workloads() {
		names = append(names, w.Name())
	}
	plan := bench.Plan(names, rounds, seed)
	planText := bench.PlanHeader + "\n"
	for _, e := range plan {
		planText += e.String() + "\n"
	}
	for name, text := range map[string]string{
		"list": strings.Join(names, "\n") + "\n", "plan": planText, "atlasbench": stubDriver,
	} {
		if err := os.WriteFile(filepath.Join(stub, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	c := campaign{dir: t.TempDir(), plan: plan}
	c.out = filepath.Join(c.dir, "campaign")
	cmd := exec.Command("bash", script, c.out)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "ATLASBENCH="+filepath.Join(stub, "atlasbench"), "STUB="+stub,
		"RUNS="+strconv.Itoa(rounds), "SEED="+strconv.FormatUint(seed, 10),
		"STUB_ROUNDS="+strconv.Itoa(rounds), "STUB_SEED="+strconv.FormatUint(seed, 10),
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(c.dir))
	cmd.Env = append(cmd.Env, env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err = cmd.Run()
	c.stderr = stderr.String()
	if ee, ok := err.(*exec.ExitError); ok {
		c.code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return c
}

func (c campaign) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(c.out, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lines(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

func TestBaselineCampaign(t *testing.T) {
	c := runBaseline(t, 2, 42, "ALLOW_DIRTY=1")
	if c.code != 0 {
		t.Fatalf("exit %d: %s", c.code, c.stderr)
	}
	if s := c.read(t, "status"); s != "complete\n" {
		t.Errorf("status %q", s)
	}
	if _, err := os.Stat(filepath.Join(c.out, "failures.txt")); !os.IsNotExist(err) {
		t.Errorf("failures.txt exists after a complete campaign: %v", err)
	}
	camp := c.read(t, "campaign.txt")
	for _, want := range []string{"master-seed: 42\n", "rounds-per-set: 2\n", "git-commit: unknown\n",
		"git-dirty: true\n", "allow-dirty: 1\n", "ATLASBENCH override", "driver-sha256: ", "result: complete, 84 invocations"} {
		if !strings.Contains(camp, want) {
			t.Errorf("campaign.txt lacks %q:\n%s", want, camp)
		}
	}
	// The manifest follows the plan exactly, one line per invocation, and
	// each invocation was its own driver process.
	man := lines(c.read(t, "manifest.tsv"))
	stubCalls := c.stubCalls(t)
	if len(man) != 1+len(c.plan) || len(stubCalls) != len(c.plan) {
		t.Fatalf("manifest has %d lines, stub saw %d calls, plan has %d entries", len(man), len(stubCalls), len(c.plan))
	}
	sets := map[string][]string{}
	for i, e := range c.plan {
		f := strings.Split(man[i+1], "\t")
		if len(f) != 11 || strings.Join(f[:5], "\t") != e.String() || f[7] != "0" || f[8] != "1000" {
			t.Fatalf("manifest line %d = %q, plan %q", i+1, man[i+1], e)
		}
		if stubCalls[i] != e.Workload {
			t.Errorf("call %d ran %s, plan says %s", i, stubCalls[i], e.Workload)
		}
		if got := c.read(t, f[10]); got != "stub stderr for "+e.Workload+"\n" {
			t.Errorf("%s = %q", f[10], got)
		}
		sets[e.Set] = append(sets[e.Set], "BenchmarkSuite/"+e.Workload+" 1 1000 ns/op 4000000 peak-rss-bytes")
	}
	for set, want := range sets {
		var got []string
		for _, l := range lines(c.read(t, set+".txt")) {
			if strings.HasPrefix(l, "BenchmarkSuite/") {
				got = append(got, l)
			}
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s.txt has %d result lines, want %d in plan order", set, len(got), len(want))
		}
	}
	if got := c.read(t, "plan.tsv"); !strings.HasPrefix(got, bench.PlanHeader+"\n") || len(lines(got)) != 1+len(c.plan) {
		t.Errorf("plan.tsv: %d lines", len(lines(got)))
	}
	load := lines(c.read(t, "loadavg.tsv"))
	if len(load) != 1+4+1 || !strings.HasPrefix(load[1], "A\t1\t") || !strings.HasPrefix(load[5], "end\t-\t") {
		t.Errorf("loadavg.tsv:\n%s", strings.Join(load, "\n"))
	}
}

func (c campaign) stubCalls(t *testing.T) []string {
	t.Helper()
	// The stub's directory is the one holding the driver named in campaign.txt.
	for _, l := range lines(c.read(t, "campaign.txt")) {
		if p, ok := strings.CutPrefix(l, "driver: "); ok {
			p, _, _ = strings.Cut(p, " ")
			b, err := os.ReadFile(filepath.Join(filepath.Dir(p), "calls"))
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				t.Fatal(err)
			}
			return lines(string(b))
		}
	}
	t.Fatal("campaign.txt names no driver")
	return nil
}

// A failed invocation stops the campaign at once: it is recorded with its
// logs, nothing after it runs, nothing is retried, and the campaign is
// invalid.
func TestBaselineFailStop(t *testing.T) {
	for _, tc := range []struct{ env, why string }{
		{"STUB_FAIL", "exit status 1"},
		{"STUB_GARBLE", "no single result line"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			var names []string
			for _, w := range bench.Workloads() {
				names = append(names, w.Name())
			}
			const first = 4 // set A, round 1, position 5: the workload's first run
			bad := bench.Plan(names, 2, 7)[first]
			c := runBaseline(t, 2, 7, "ALLOW_DIRTY=1", tc.env+"="+bad.Workload)
			if c.code != 1 || !strings.Contains(c.stderr, "campaign invalid") {
				t.Fatalf("exit %d: %s", c.code, c.stderr)
			}
			if s := c.read(t, "status"); s != "invalid\n" {
				t.Errorf("status %q", s)
			}
			f := c.read(t, "failures.txt")
			for _, want := range []string{tc.why, "workload: " + bad.Workload, "round: 1", "position: 5",
				"invocations-completed-before: " + strconv.Itoa(first) + " of 84", "stderr follows:", "stub stderr for " + bad.Workload} {
				if !strings.Contains(f, want) {
					t.Errorf("failures.txt lacks %q:\n%s", want, f)
				}
			}
			man := lines(c.read(t, "manifest.tsv"))
			if len(man) != 1+first+1 || len(c.stubCalls(t)) != first+1 {
				t.Errorf("manifest has %d lines, stub saw %d calls; want the run to stop after invocation %d",
					len(man), len(c.stubCalls(t)), first+1)
			}
			last := strings.Split(man[len(man)-1], "\t")
			if last[4] != bad.Workload || last[8] != "NA" {
				t.Errorf("last manifest line %q", man[len(man)-1])
			}
			if _, err := os.Stat(filepath.Join(c.out, last[10])); err != nil {
				t.Errorf("failed invocation's stderr not kept: %v", err)
			}
			if strings.Contains(c.read(t, "campaign.txt"), "result: complete") {
				t.Error("an invalid campaign reads as complete")
			}
		})
	}
}

// The script never reuses a directory and refuses a tree that is not clean
// unless ALLOW_DIRTY=1.
func TestBaselineRefuses(t *testing.T) {
	c := runBaseline(t, 1, 1) // no ALLOW_DIRTY, outside a git checkout
	if c.code != 2 || !strings.Contains(c.stderr, "not clean") {
		t.Errorf("dirty tree: exit %d: %s", c.code, c.stderr)
	}
	if _, err := os.Stat(c.out); !os.IsNotExist(err) {
		t.Errorf("refused campaign created %s: %v", c.out, err)
	}

	first := runBaseline(t, 1, 1, "ALLOW_DIRTY=1")
	if first.code != 0 {
		t.Fatalf("exit %d: %s", first.code, first.stderr)
	}
	before := first.read(t, "manifest.tsv")
	cmd := exec.Command("bash", "baseline.sh", first.out)
	cmd.Env = append(os.Environ(), "ALLOW_DIRTY=1", "ATLASBENCH=/nonexistent")
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "exists") {
		t.Errorf("existing directory: %v: %s", err, out)
	}
	if first.read(t, "manifest.tsv") != before {
		t.Error("existing campaign directory was modified")
	}
}
