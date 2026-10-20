package bench_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stevenstank/atlas/internal/bench"
)

// The baseline.sh tests run the real script with stand-ins on PATH, from
// temporary directories, so that nothing is measured and nothing touches
// this repository or its git data:
//   - git answers only the three queries the script makes, about a fake
//     worktree; it never runs the real git;
//   - go "builds" the driver by copying the stub, and prints a fake
//     `go version -m`;
//   - the driver stub prints the -list and -plan files of the fixture, and
//     for -bench a fake result line.

const fakeGit = `#!/usr/bin/env bash
args=()
while (($#)); do
	case $1 in
	-C) echo "$2" >>"$STUB/git-dirs"; shift 2 ;;
	--no-optional-locks) shift ;;
	*) args+=("$1"); shift ;;
	esac
done
case "${args[*]}" in
"rev-parse --show-toplevel") [[ -n $FAKE_TOP ]] || exit 128; echo "$FAKE_TOP" ;;
"rev-parse HEAD") echo "$FAKE_HEAD" ;;
"status --porcelain") cat "$STUB/porcelain" ;;
*) echo "fake git: unexpected: $*" >&2; exit 99 ;;
esac
`

// The build runs under env -i, so fake go finds the fixture from its own
// path, and records the environment it was given in build-env.
const fakeGo = `#!/usr/bin/env bash
stub=$(dirname "$(dirname "$(realpath "$0")")")
case "$1 $2" in
"build -o")
	env >"$stub/build-env"
	[[ $# -eq 4 && $4 == ./internal/bench/cmd/atlasbench && $PWD == "$(cat "$stub/top")" ]] || { echo "fake go: unexpected: $* in $PWD" >&2; exit 99; }
	[[ ! -e $stub/build-fail ]] || { echo "fake go: compile error" >&2; exit 1; }
	cp "$stub/atlasbench" "$3" ;;
"version -m")
	[[ -f $stub/buildinfo ]] || { echo "$3: could not read Go build info" >&2; exit 1; }
	cat "$stub/buildinfo" ;;
*) echo "fake go: unexpected: $*" >&2; exit 99 ;;
esac
`

// The driver stub logs every argument list it gets, one line per process.
// -bench misbehaves in STUB_MODE for the workload STUB_TARGET.
const stubDriver = `#!/usr/bin/env bash
echo "$*" >>"$STUB/calls"
case "$1" in
-list) [[ -z ${STUB_LIST_FAIL:-} ]] || { echo "list broke" >&2; exit 3; }; cat "$STUB/list" ;;
-plan) [[ -z ${STUB_PLAN_FAIL:-} ]] || { echo "plan broke" >&2; exit 4; }; cat "$STUB/plan" ;;
-bench)
	[[ $# -eq 4 && $3 == -format && $4 == bench ]] || exit 9
	echo "stub stderr for $2" >&2
	line="BenchmarkSuite/$2 1 1000 ns/op 4000000 peak-rss-bytes"
	mode=
	[[ $2 == "${STUB_TARGET:-}" ]] && mode=$STUB_MODE
	case $mode in
	fail) echo "atlasbench: $2: bench: wrong result" >&2; exit 1 ;;
	then-fail) echo "$line"; exit 1 ;;
	none) echo "goos: linux" ;;
	two) echo "$line"; echo "$line" ;;
	badns) echo "BenchmarkSuite/$2 1 1.5e3 ns/op" ;;
	wrong) echo "BenchmarkSuite/other 1 1000 ns/op" ;;
	*) echo "goos: linux"; echo "$line" ;;
	esac ;;
*) exit 9 ;;
esac
`

const fakeHead = "0123456789abcdef0123456789abcdef01234567"

// goodBuildInfo is `go version -m` of a default build at fakeHead, in the
// format of the real atlasbench binary of 2026-10-06.
const goodBuildInfo = "x: go1.26.1\n\tpath\tgithub.com/stevenstank/atlas/internal/bench/cmd/atlasbench\n" +
	"\tmod\tgithub.com/stevenstank/atlas\t(devel)\t\n\tbuild\t-buildmode=exe\n\tbuild\t-compiler=gc\n" +
	"\tbuild\tCGO_ENABLED=1\n\tbuild\tCGO_CFLAGS=\n\tbuild\tCGO_CPPFLAGS=\n\tbuild\tCGO_CXXFLAGS=\n" +
	"\tbuild\tCGO_LDFLAGS=\n\tbuild\tGOARCH=amd64\n\tbuild\tGOOS=linux\n\tbuild\tGOAMD64=v1\n" +
	"\tbuild\tvcs=git\n\tbuild\tvcs.revision=" + fakeHead + "\n\tbuild\tvcs.modified=false\n"

type fixture struct {
	t           *testing.T
	root        string            // holds the stub, the fake worktree, and data/
	stub, top   string            // stub directory; fake worktree (empty)
	script, cwd string            // how baseline.sh is invoked, and from where
	env         map[string]string // test controls for the child process
	base        []string          // the environment to start from
	names, plan []string          // -list names; -plan lines
}

func newFixture(t *testing.T, rounds int, seed uint64) *fixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("baseline.sh needs GNU coreutils and /proc")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("baseline.sh")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, stub: filepath.Join(root, "stub"), top: filepath.Join(root, "repo"),
		script: script, cwd: root, base: os.Environ()}
	for _, w := range bench.Workloads() {
		f.names = append(f.names, w.Name())
	}
	f.plan = []string{bench.PlanHeader}
	for _, e := range bench.Plan(f.names, rounds, seed) {
		f.plan = append(f.plan, e.String())
	}
	f.env = map[string]string{"RUNS": strconv.Itoa(rounds), "SEED": strconv.FormatUint(seed, 10)}
	for _, d := range []string{"stub/bin", "repo", "data"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.write("bin/git", fakeGit)
	f.write("bin/go", fakeGo)
	f.write("atlasbench", stubDriver)
	f.write("porcelain", "")
	f.write("buildinfo", goodBuildInfo)
	return f
}

func (f *fixture) write(name, text string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.stub, name), []byte(text), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

type result struct {
	out    string // the campaign directory
	code   int
	stderr string
}

// run runs baseline.sh OUTDIR. Every variable that steers the script, bash
// (BASH_ENV, ENV, CDPATH), or the stand-ins is dropped from f.base first, so
// the caller's environment cannot change the outcome; then f.env and the
// fixture's own settings are added.
func (f *fixture) run(outdir string) result {
	f.t.Helper()
	f.write("list", strings.Join(f.names, "\n")+"\n")
	f.write("plan", strings.Join(f.plan, "\n")+"\n")
	f.write("top", f.top)
	var env []string
	for _, kv := range f.base {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "STUB") || strings.HasPrefix(k, "FAKE_") || strings.HasPrefix(k, "GIT_") ||
			slices.Contains([]string{"ALLOW_DIRTY", "ATLASBENCH", "SEED", "RUNS", "PATH", "BASH_ENV", "ENV", "CDPATH"}, k) {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "PATH="+filepath.Join(f.stub, "bin")+":"+os.Getenv("PATH"),
		"STUB="+f.stub, "FAKE_TOP="+f.top, "FAKE_HEAD="+fakeHead)
	for k, v := range f.env {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command("bash", f.script, outdir)
	cmd.Dir = f.cwd
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	r := result{out: outdir, stderr: stderr.String()}
	if ee, ok := err.(*exec.ExitError); ok {
		r.code = ee.ExitCode()
	} else if err != nil {
		f.t.Fatal(err)
	}
	return r
}

// benchCalls returns the workloads the driver stub was asked to run.
func (f *fixture) benchCalls() []string {
	b, _ := os.ReadFile(filepath.Join(f.stub, "calls"))
	var calls []string
	for _, l := range lines(string(b)) {
		if w, ok := strings.CutPrefix(l, "-bench "); ok {
			calls = append(calls, strings.TrimSuffix(w, " -format bench"))
		}
	}
	return calls
}

func (r result) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.out, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func resultLines(s string) (n int) {
	for _, l := range lines(s) {
		if strings.HasPrefix(l, "BenchmarkSuite/") {
			n++
		}
	}
	return n
}

func TestBaselineCampaign(t *testing.T) {
	f := newFixture(t, 2, 42)
	r := f.run(filepath.Join(f.root, "data", "campaign"))
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if s := r.read(t, "status"); s != "complete\n" {
		t.Errorf("status %q", s)
	}
	if _, err := os.Stat(filepath.Join(r.out, "failures.txt")); !os.IsNotExist(err) {
		t.Errorf("failures.txt exists after a complete campaign: %v", err)
	}
	camp := r.read(t, "campaign.txt")
	for _, want := range []string{"master-seed: 42\n", "rounds-per-set: 2\n", "git-commit: " + fakeHead + "\n",
		"git-dirty: false\n", "(built from " + f.top + ")", "driver-vcs-revision: " + fakeHead + "\n",
		"provenance: ok", "result: complete, 84 invocations"} {
		if !strings.Contains(camp, want) {
			t.Errorf("campaign.txt lacks %q:\n%s", want, camp)
		}
	}
	// The manifest follows the plan exactly, one line and one driver process
	// per invocation, and the logs and result files match it.
	man := lines(r.read(t, "manifest.tsv"))
	calls := f.benchCalls()
	if len(man) != len(f.plan) || len(calls) != len(f.plan)-1 {
		t.Fatalf("manifest has %d lines, stub saw %d runs, plan has %d entries", len(man), len(calls), len(f.plan)-1)
	}
	perSet := map[string]int{}
	for i, p := range f.plan[1:] {
		fields := strings.Split(man[i+1], "\t")
		if len(fields) != 11 || strings.Join(fields[:5], "\t") != p || fields[7] != "0" || fields[8] != "1000" {
			t.Fatalf("manifest line %d = %q, plan %q", i+1, man[i+1], p)
		}
		w := strings.Split(p, "\t")[4]
		if calls[i] != w {
			t.Errorf("run %d was %s, plan says %s", i, calls[i], w)
		}
		if got := r.read(t, fields[10]); got != "stub stderr for "+w+"\n" {
			t.Errorf("%s = %q", fields[10], got)
		}
		perSet[fields[0]]++
	}
	for set, n := range perSet {
		if got := resultLines(r.read(t, set+".txt")); got != n {
			t.Errorf("%s.txt has %d results, want %d", set, got, n)
		}
	}
	if load := lines(r.read(t, "loadavg.tsv")); len(load) != 1+4+1 || !strings.HasPrefix(load[5], "end\t-\t") {
		t.Errorf("loadavg.tsv:\n%s", strings.Join(load, "\n"))
	}
}

// A preflight refusal exits 2 and creates nothing, whatever the caller's
// environment holds; in particular an inherited ALLOW_DIRTY or GIT_DIR has
// no effect.
func TestBaselineRefusals(t *testing.T) {
	polluted := append(os.Environ(), "ALLOW_DIRTY=1", "GIT_DIR=/nonexistent", "STUB_TARGET=B1",
		"STUB_MODE=fail", "ATLASBENCH=/bin/true")
	polluted = append(polluted, hostileShell(t)...)
	for _, inherited := range []struct {
		name string
		env  []string
	}{{"clean", os.Environ()}, {"polluted", polluted}} {
		for _, c := range []struct {
			name, want string
			setup      func(f *fixture) string // returns OUTDIR
		}{
			{"dirty tree", "not clean", func(f *fixture) string {
				f.write("porcelain", " M core/store.go\n")
				return filepath.Join(f.root, "data", "c")
			}},
			{"not a checkout", "not in a git checkout", func(f *fixture) string {
				f.top = ""
				return filepath.Join(f.root, "data", "c")
			}},
			{"inside worktree", "inside the worktree", func(f *fixture) string { return filepath.Join(f.top, "c") }},
			{"inside via ..", "inside the worktree", func(f *fixture) string {
				return filepath.Join(f.root, "data") + "/../repo/sub/c"
			}},
			{"inside via symlink", "inside the worktree", func(f *fixture) string {
				if err := os.Symlink(f.top, filepath.Join(f.root, "link")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(f.root, "link", "c")
			}},
			{"supplied binary", "ATLASBENCH is not supported", func(f *fixture) string {
				f.env["ATLASBENCH"] = filepath.Join(f.stub, "atlasbench") // with valid build info
				return filepath.Join(f.root, "data", "c")
			}},
			{"BASH_ENV", "BASH_ENV is set; unset it", func(f *fixture) string {
				f.write("empty", "") // harmless: bash runs it before the script starts
				f.env["BASH_ENV"] = filepath.Join(f.stub, "empty")
				return filepath.Join(f.root, "data", "c")
			}},
			{"ENV", "baseline.sh: ENV is set; unset it", func(f *fixture) string {
				f.write("empty", "")
				f.env["ENV"] = filepath.Join(f.stub, "empty")
				return filepath.Join(f.root, "data", "c")
			}},
			{"existing directory", "exists", func(f *fixture) string {
				d := filepath.Join(f.root, "data", "old")
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
				return d
			}},
		} {
			t.Run(inherited.name+"/"+c.name, func(t *testing.T) {
				f := newFixture(t, 1, 1)
				f.base = inherited.env
				out := c.setup(f)
				r := f.run(out)
				if r.code != 2 || !strings.Contains(r.stderr, c.want) {
					t.Errorf("exit %d, want 2 and %q: %s", r.code, c.want, r.stderr)
				}
				for _, d := range []string{f.top, filepath.Join(f.root, "data")} {
					if entries, _ := os.ReadDir(d); len(entries) > 0 && (c.name != "existing directory" || len(entries) != 1) {
						t.Errorf("%s gained entries: %v", d, entries)
					}
				}
				if b, err := os.ReadFile(filepath.Join(f.stub, "calls")); err == nil {
					t.Errorf("the driver ran: %s", b)
				}
			})
		}
	}
}

// Seeds follow bench.ParseSeed exactly: what is accepted is recorded and
// passed to -plan unchanged; anything else is refused before OUTDIR exists.
func TestBaselineSeeds(t *testing.T) {
	for _, c := range []struct {
		seed string
		ok   bool
	}{
		{"0", true}, {"42", true}, {"18446744073709551615", true}, {"unset", true},
		{"", false}, {"00", false}, {"010", false}, {"08", false}, {"+1", false}, {"-1", false},
		{" 42", false}, {"42 ", false}, {"4\n2", false}, {"0x10", false}, {"1e3", false}, {"1_000", false},
		{"١", false}, {"18446744073709551616", false}, {"99999999999999999999", false},
	} {
		t.Run(strconv.Quote(c.seed), func(t *testing.T) {
			if c.seed != "unset" {
				if _, err := bench.ParseSeed(c.seed); (err == nil) != c.ok {
					t.Errorf("bench.ParseSeed(%q) error %v, want ok=%v", c.seed, err, c.ok)
				}
			}
			f := newFixture(t, 1, 3)
			f.env["STUB_TARGET"], f.env["STUB_MODE"] = f.names[0], "fail" // stop at the first workload
			if c.seed == "unset" {
				delete(f.env, "SEED")
			} else {
				f.env["SEED"] = c.seed
			}
			r := f.run(filepath.Join(f.root, "data", "c"))
			if !c.ok {
				if r.code != 2 || !strings.Contains(r.stderr, "SEED must be") {
					t.Errorf("exit %d: %s", r.code, r.stderr)
				}
				if _, err := os.Stat(r.out); !os.IsNotExist(err) {
					t.Errorf("OUTDIR was created")
				}
				return
			}
			// The stub's plan is fixed, so the run fails at the planned
			// workload; the seed must already be recorded and used.
			seed := c.seed
			if c.seed == "unset" {
				camp := r.read(t, "campaign.txt")
				seed, _, _ = strings.Cut(strings.SplitN(camp, "master-seed: ", 2)[1], "\n")
				if _, err := bench.ParseSeed(seed); err != nil {
					t.Errorf("default seed %q is not canonical", seed)
				}
			}
			calls, _ := os.ReadFile(filepath.Join(f.stub, "calls"))
			if !strings.Contains(r.read(t, "campaign.txt"), "master-seed: "+seed+"\n") ||
				!strings.Contains(string(calls), "-plan -seed "+seed+" -rounds 1\n") {
				t.Errorf("seed %q not recorded and used as is:\n%s", seed, calls)
			}
		})
	}
}

func TestBaselineRuns(t *testing.T) {
	for _, runs := range []string{"", "0", "01", "-1", "x", "1.5", "10000"} {
		f := newFixture(t, 1, 1)
		f.env["RUNS"] = runs
		r := f.run(filepath.Join(f.root, "data", "c"))
		if _, err := os.Stat(r.out); r.code != 2 || !strings.Contains(r.stderr, "RUNS must be") || !os.IsNotExist(err) {
			t.Errorf("RUNS=%q: exit %d, OUTDIR %v: %s", runs, r.code, err, r.stderr)
		}
	}
}

// Of two campaigns started at once on one OUTDIR, exactly one claims it and
// the other leaves it alone.
func TestBaselineConcurrentClaim(t *testing.T) {
	shared := t.TempDir()
	for i := range 3 {
		out := filepath.Join(shared, "c"+strconv.Itoa(i))
		fs := []*fixture{newFixture(t, 1, 5), newFixture(t, 1, 5)}
		rs := make([]result, 2)
		var wg sync.WaitGroup
		for j := range fs {
			wg.Add(1)
			go func() { defer wg.Done(); rs[j] = fs[j].run(out) }()
		}
		wg.Wait()
		win := slices.IndexFunc(rs, func(r result) bool { return r.code == 0 })
		if win < 0 || rs[1-win].code != 2 || len(fs[1-win].benchCalls()) != 0 {
			t.Fatalf("exits %d and %d: %s %s", rs[0].code, rs[1].code, rs[0].stderr, rs[1].stderr)
		}
		if man := lines(rs[win].read(t, "manifest.tsv")); len(man) != 1+42 {
			t.Errorf("the winner's manifest has %d lines", len(man))
		}
	}
}

// A failed setup step stops the campaign before any workload runs, with its
// phase, exit status, and stderr in failures.txt.
func TestBaselineSetupFailures(t *testing.T) {
	for _, c := range []struct {
		name, want, stderrFile string
		setup                  func(f *fixture)
	}{
		{"build", "setup: build exited with status 1", "setup-build.stderr",
			func(f *fixture) { f.write("build-fail", "1") }},
		{"no build info", "setup: buildinfo exited with status 1", "setup-buildinfo.stderr",
			func(f *fixture) { os.Remove(filepath.Join(f.stub, "buildinfo")) }},
		{"revision mismatch", "vcs.revision is 'ffffffffffffffffffffffffffffffffffffffff'", "", func(f *fixture) {
			f.write("buildinfo", "\tbuild\tvcs.revision=ffffffffffffffffffffffffffffffffffffffff\n\tbuild\tvcs.modified=false\n")
		}},
		{"dirty build", "vcs.modified is 'true'", "", func(f *fixture) {
			f.write("buildinfo", "\tbuild\tvcs.revision="+fakeHead+"\n\tbuild\tvcs.modified=true\n")
		}},
		{"no vcs stamp", "vcs.revision is 'none'", "", func(f *fixture) { f.write("buildinfo", "x: go1.26.1\n") }},
		{"list", "setup: list exited with status 3", "setup-list.stderr", func(f *fixture) { f.env["STUB_LIST_FAIL"] = "1" }},
		{"list count", "-list gave 20 workloads", "", func(f *fixture) { f.names = f.names[:20] }},
		{"plan", "setup: plan exited with status 4", "setup-plan.stderr", func(f *fixture) { f.env["STUB_PLAN_FAIL"] = "1" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, 1, 9)
			c.setup(f)
			r := f.run(filepath.Join(f.root, "data", "c"))
			if r.code != 1 || r.read(t, "status") != "invalid\n" {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			fail := r.read(t, "failures.txt")
			if !strings.Contains(fail, c.want) {
				t.Errorf("failures.txt lacks %q:\n%s", c.want, fail)
			}
			if c.stderrFile != "" {
				se := r.read(t, "logs/"+c.stderrFile)
				if se == "" || !strings.Contains(fail, "stderr: logs/"+c.stderrFile) || !strings.Contains(fail, strings.TrimSpace(se)) {
					t.Errorf("stderr %q not kept and quoted:\n%s", se, fail)
				}
			}
			if camp := r.read(t, "campaign.txt"); !strings.Contains(camp, "result: invalid") || strings.Contains(camp, "provenance: ok") != (c.name == "list" || c.name == "list count" || c.name == "plan") {
				t.Errorf("campaign.txt:\n%s", camp)
			}
			if calls := f.benchCalls(); len(calls) != 0 {
				t.Errorf("workloads ran: %v", calls)
			}
		})
	}
}

// Each mutation breaks one property of a valid plan; every one must be
// rejected before any workload runs.
func TestBaselinePlanValidation(t *testing.T) {
	type plan = []string
	field := func(p plan, row, col int, v string) plan {
		f := strings.Split(p[row], "\t")
		f[col] = v
		p[row] = strings.Join(f, "\t")
		return p
	}
	const n = 21 // rows per round; row 1+k*n starts round k+1 (A1, A2, B1, B2)
	for _, c := range []struct {
		name   string
		mutate func(p plan) plan
	}{
		{"empty", func(p plan) plan { return nil }},
		{"header", func(p plan) plan { p[0] = "set\tround\tpos\tseed\tworkload"; return p }},
		{"missing row", func(p plan) plan { return p[:len(p)-1] }},
		{"extra row", func(p plan) plan { return append(p, p[len(p)-1]) }},
		{"extra field", func(p plan) plan { p[5] += "\tx"; return p }},
		{"blank line", func(p plan) plan { return slices.Insert(p, 3, "") }},
		{"unknown set", func(p plan) plan { return field(p, 1, 0, "C") }},
		{"B before A", func(p plan) plan {
			return slices.Concat(p[:1], p[1+2*n:1+3*n], p[1+n:1+2*n], p[1:1+n], p[1+3*n:])
		}},
		{"round out of range", func(p plan) plan {
			for r := 1 + n; r < 1+2*n; r++ {
				p = field(p, r, 1, "3")
			}
			return p
		}},
		{"round 01", func(p plan) plan { return field(p, 1, 1, "01") }},
		{"position gap", func(p plan) plan { return field(p, 2, 2, "3") }},
		{"position order", func(p plan) plan { p[2], p[3] = p[3], p[2]; return p }},
		{"repeated workload", func(p plan) plan { return field(p, 2, 4, strings.Split(p[1], "\t")[4]) }},
		{"unknown workload", func(p plan) plan { return field(p, 4, 4, "B9") }},
		{"CRLF", func(p plan) plan { p[4] += "\r"; return p }},
		{"seed varies in round", func(p plan) plan { return field(p, 7, 3, "123") }},
		{"seed reused", func(p plan) plan {
			s := strings.Split(p[1], "\t")[3]
			for r := 1 + n; r < 1+2*n; r++ {
				p = field(p, r, 3, s)
			}
			return p
		}},
		{"seed leading zero", func(p plan) plan {
			for r := 1; r < 1+n; r++ {
				p = field(p, r, 3, "07")
			}
			return p
		}},
		{"seed overflow", func(p plan) plan {
			for r := 1; r < 1+n; r++ {
				p = field(p, r, 3, "18446744073709551616")
			}
			return p
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, 2, 11)
			f.plan = c.mutate(slices.Clone(f.plan))
			r := f.run(filepath.Join(f.root, "data", "c"))
			if r.code != 1 || r.read(t, "status") != "invalid\n" {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			if fail := r.read(t, "failures.txt"); !strings.Contains(fail, "phase: plan validation") {
				t.Errorf("failures.txt:\n%s", fail)
			}
			if calls := f.benchCalls(); len(calls) != 0 {
				t.Errorf("workloads ran: %v", calls)
			}
		})
	}
}

// A failed invocation stops the campaign at once: it is recorded with its
// logs, nothing after it runs or is retried, and only the earlier successful
// results are in the result files.
func TestBaselineInvocationFailures(t *testing.T) {
	for _, c := range []struct{ mode, why string }{
		{"fail", "exit status 1"}, {"then-fail", "exit status 1"}, {"none", "no single result line"},
		{"two", "no single result line"}, {"badns", "no single result line"}, {"wrong", "no single result line"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			f := newFixture(t, 2, 7)
			bad := strings.Split(f.plan[5], "\t") // set A, round 1, position 5: its first run
			f.env["STUB_TARGET"], f.env["STUB_MODE"] = bad[4], c.mode
			r := f.run(filepath.Join(f.root, "data", "c"))
			if r.code != 1 || r.read(t, "status") != "invalid\n" {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			fail := r.read(t, "failures.txt")
			for _, want := range []string{c.why, "workload: " + bad[4], "round: 1", "position: 5", "seed: " + bad[3],
				"invocations-completed-before: 4 of 84", "stderr follows:", "stub stderr for " + bad[4]} {
				if !strings.Contains(fail, want) {
					t.Errorf("failures.txt lacks %q:\n%s", want, fail)
				}
			}
			man := lines(r.read(t, "manifest.tsv"))
			if len(man) != 1+5 || len(f.benchCalls()) != 5 {
				t.Errorf("manifest has %d lines, stub saw %d runs; want 5 each", len(man)-1, len(f.benchCalls()))
			}
			last := strings.Split(man[len(man)-1], "\t")
			if last[4] != bad[4] || r.read(t, last[10]) == "" {
				t.Errorf("last manifest line %q, stderr not kept", man[len(man)-1])
			}
			if a, b := resultLines(r.read(t, "A.txt")), resultLines(r.read(t, "B.txt")); a != 4 || b != 0 {
				t.Errorf("A.txt has %d results and B.txt %d; want 4 and 0", a, b)
			}
			if strings.Contains(r.read(t, "campaign.txt"), "result: complete") {
				t.Error("an invalid campaign reads as complete")
			}
		})
	}
}

// hostileShell returns bash startup and cd variables that would break a run
// if they reached it: a BASH_ENV (and ENV) file that sets RUNS=0 and a
// non-canonical SEED, and a CDPATH holding another internal/bench.
func hostileShell(t *testing.T) []string {
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "other", "internal", "bench"), 0o755); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(d, "evil.sh")
	if err := os.WriteFile(evil, []byte("RUNS=0\nSEED=010\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{"BASH_ENV=" + evil, "ENV=" + evil, "CDPATH=" + filepath.Join(d, "other")}
}

// The driver is built in the documented environment whatever the caller's
// environment holds: inherited build variables (GOFLAGS, a go env file,
// GOWORK, ...) never reach go build, and a hostile shell environment does not
// reach the script.
func TestBaselineBuild(t *testing.T) {
	f := newFixture(t, 1, 13)
	f.base = append(os.Environ(), "GOFLAGS=-race -overlay=/tmp/overlay.json", "GOENV=/nonexistent/env",
		"GOWORK=/nonexistent/go.work", "GOEXPERIMENT=arenas", "GOAMD64=v3", "CGO_CFLAGS=-O0",
		"GOTOOLCHAIN=go1.99.0", "GOROOT=/nonexistent")
	f.base = append(f.base, hostileShell(t)...)
	r := f.run(filepath.Join(f.root, "data", "c"))
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(f.stub, "build-env"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range lines(string(b)) {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k, want := range map[string]string{"GOENV": "off", "GOFLAGS": "", "GOWORK": "off", "GOTOOLCHAIN": "local",
		"HOME": os.Getenv("HOME")} {
		if v, ok := got[k]; !ok || v != want {
			t.Errorf("go build saw %s=%q (set %v), want %q", k, v, ok, want)
		}
	}
	for k := range got {
		if !slices.Contains([]string{"PATH", "HOME", "GOENV", "GOFLAGS", "GOWORK", "GOTOOLCHAIN", "PWD", "SHLVL", "_"}, k) {
			t.Errorf("go build inherited %s=%s", k, got[k])
		}
	}
	camp := r.read(t, "campaign.txt")
	for _, want := range []string{"build-command: go build -o " + r.out + "/atlasbench ./internal/bench/cmd/atlasbench\n",
		" GOENV=off GOFLAGS= GOWORK=off GOTOOLCHAIN=local\n", "driver-toolchain: go1.26.1\n",
		"driver-build-settings: -buildmode=exe; -compiler=gc; CGO_ENABLED=1; CGO_CFLAGS=;",
		"build-configuration: ok", "rounds-per-set: 1\n", "master-seed: 13\n", "result: complete"} {
		if !strings.Contains(camp, want) {
			t.Errorf("campaign.txt lacks %q:\n%s", want, camp)
		}
	}
}

// Resolving the script's own directory must not go through CDPATH, which
// could print or pick another checkout's internal/bench.
func TestBaselineCDPATH(t *testing.T) {
	f := newFixture(t, 1, 17)
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if here, err = filepath.EvalSymlinks(here); err != nil {
		t.Fatal(err)
	}
	f.cwd, f.script = filepath.Dir(filepath.Dir(here)), "internal/bench/baseline.sh"
	f.env["CDPATH"] = strings.TrimPrefix(hostileShell(t)[2], "CDPATH=")
	r := f.run(filepath.Join(f.root, "data", "c"))
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	b, _ := os.ReadFile(filepath.Join(f.stub, "git-dirs"))
	if dirs := lines(string(b)); len(dirs) == 0 || dirs[0] != here {
		t.Errorf("the script looked for its checkout in %q, want %q", dirs, here)
	}
}

// The build metadata must show the default configuration; each mutation of
// a default build's metadata is refused before any workload runs.
func TestBaselineBuildConfiguration(t *testing.T) {
	add := func(line string) func(string) string { return func(s string) string { return s + line + "\n" } }
	swap := func(old, new string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, new, 1) }
	}
	for _, c := range []struct {
		name   string
		mutate func(string) string
	}{
		{"race", add("\tbuild\t-race=true")},
		{"gcflags", add("\tbuild\t-gcflags=all=-N -l")},
		{"ldflags", add("\tbuild\t-ldflags=-s -w")},
		{"tags", add("\tbuild\t-tags=netgo")},
		{"trimpath", add("\tbuild\t-trimpath=true")},
		{"cover", add("\tbuild\t-cover=true")},
		{"asan", add("\tbuild\t-asan=true")},
		{"msan", add("\tbuild\t-msan=true")},
		{"pgo", add("\tbuild\t-pgo=/tmp/default.pgo")},
		{"GOEXPERIMENT", add("\tbuild\tGOEXPERIMENT=arenas")},
		{"GOFIPS140", add("\tbuild\tGOFIPS140=v1.0.0")},
		{"unknown setting", add("\tbuild\tFOO=bar")},
		{"dependency", add("\tdep\texample.com/x\tv1.0.0\th1:abc=")},
		{"GOAMD64", swap("GOAMD64=v1", "GOAMD64=v3")},
		{"CGO_CFLAGS", swap("CGO_CFLAGS=\n", "CGO_CFLAGS=-O0\n")},
		{"CGO_LDFLAGS spaces", swap("CGO_LDFLAGS=\n", "CGO_LDFLAGS= -lfoo\n")},
		{"buildmode", swap("-buildmode=exe", "-buildmode=pie")},
		{"compiler missing", swap("\tbuild\t-compiler=gc\n", "")},
		{"other package", swap("cmd/atlasbench\n", "cmd/other\n")},
		{"no path", swap("\tpath\tgithub.com/stevenstank/atlas/internal/bench/cmd/atlasbench\n", "")},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, 1, 19)
			f.write("buildinfo", c.mutate(goodBuildInfo))
			r := f.run(filepath.Join(f.root, "data", "c"))
			if r.code != 1 || r.read(t, "status") != "invalid\n" {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			if fail := r.read(t, "failures.txt"); !strings.Contains(fail, "phase: build configuration") {
				t.Errorf("failures.txt:\n%s", fail)
			}
			if camp := r.read(t, "campaign.txt"); strings.Contains(camp, "build-configuration: ok") {
				t.Errorf("campaign.txt claims the build configuration:\n%s", camp)
			}
			if calls := f.benchCalls(); len(calls) != 0 {
				t.Errorf("workloads ran: %v", calls)
			}
		})
	}
}
