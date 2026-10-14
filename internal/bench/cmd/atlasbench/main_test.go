package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stevenstank/atlas/internal/bench"
)

// invoke runs the driver and parses its key=value output.
func invoke(t *testing.T, args ...string) (code int, kv map[string]string, keys []string, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	kv = map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = v
			keys = append(keys, k)
		}
	}
	return code, kv, keys, errb.String()
}

func TestExhaustedRun(t *testing.T) {
	code, kv, keys, stderr := invoke(t, "-bench", "B2/k=10")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for k, want := range map[string]string{
		"benchmark": "B2/k=10", "param-k": "10", "param-d": "4", "status": "Exhausted", "states": "10000",
		"transitions": "36000", "invariant": "NA", "trace-length": "NA", "time-to-first-counterexample-ns": "NA",
		"violation-detection-ns": "NA",
		"replay":                 "NA", "cpu-profile": "off", "mem-profile": "off", "driver-version": driverVersion,
		"atlas-suite": bench.SuiteVersion,
	} {
		if kv[k] != want {
			t.Errorf("%s = %q, want %q", k, kv[k], want)
		}
	}
	for _, k := range []string{"wall-time-ns", "peak-rss-bytes", "alloc-bytes", "allocs", "gc-cycles", "gc-cpu-fraction"} {
		if n, err := strconv.ParseFloat(kv[k], 64); err != nil || n < 0 || (k == "wall-time-ns" && n == 0) {
			t.Errorf("%s = %q", k, kv[k])
		}
	}
	// The schema's order is fixed: environment first, the result last.
	if keys[0] != "atlas-suite" || keys[len(keys)-1] != "replay" {
		t.Errorf("key order: %v", keys)
	}
}

// For a Violation, time-to-first-counterexample-ns is the BENCHMARKS.md §2
// interval (until Run returns, trace included), which is the wall time.
// violation-detection-ns comes from the hook, which fires during check.Run
// before the trace is rebuilt, so it is positive and at most that. The trace
// is replayed.
func TestViolationRun(t *testing.T) {
	code, kv, _, stderr := invoke(t, "-bench", "B4")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if kv["status"] != "Violation" || kv["invariant"] != "NotFour" || kv["trace-length"] != "6" ||
		kv["states"] != "13" || kv["replay"] != "ok" || kv["param-big"] != "5" {
		t.Errorf("%v", kv)
	}
	detect, err1 := strconv.ParseInt(kv["violation-detection-ns"], 10, 64)
	wall, err2 := strconv.ParseInt(kv["wall-time-ns"], 10, 64)
	if err1 != nil || err2 != nil || detect <= 0 || detect > wall || kv["time-to-first-counterexample-ns"] != kv["wall-time-ns"] {
		t.Errorf("violation-detection-ns = %q, time-to-first-counterexample-ns = %q, wall-time-ns = %q",
			kv["violation-detection-ns"], kv["time-to-first-counterexample-ns"], kv["wall-time-ns"])
	}
}

// -format bench prints only environment configuration lines and one
// benchmark line; violation-detection-ns appears only for a Violation.
func TestBenchFormat(t *testing.T) {
	for name, wantDetect := range map[string]bool{"B1": false, "B4": true} {
		var out bytes.Buffer
		if code := run([]string{"-bench", name, "-format", "bench"}, &out, &bytes.Buffer{}); code != 0 {
			t.Fatalf("%s: exit %d", name, code)
		}
		lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
		last := lines[len(lines)-1]
		f := strings.Fields(last)
		if f[0] != "BenchmarkSuite/"+name || f[1] != "1" || f[3] != "ns/op" || !strings.Contains(last, " peak-rss-bytes") ||
			!strings.Contains(last, " B/op") || !strings.Contains(last, " allocs/op") || strings.Contains(last, "violation-detection-ns") != wantDetect {
			t.Errorf("%s: %q", name, last)
		}
		for _, l := range lines[:len(lines)-1] {
			if k, _, ok := strings.Cut(l, ": "); !ok || strings.HasPrefix(k, "param-") || k == "benchmark" {
				t.Errorf("%s: config line %q", name, l)
			}
		}
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"-bench", "B1", "-format", "json"},
		{"-bench", "B9"}, {"-bench", "B2"}, {"-bench", "B5/n=7"}, {}, {"-bench", "B1", "extra"}, {"-nosuchflag"},
	} {
		if code, kv, _, stderr := invoke(t, args...); code != 2 || len(kv) != 0 || stderr == "" {
			t.Errorf("%q: exit %d, stdout %v, stderr %q", args, code, kv, stderr)
		}
	}
	if _, _, _, stderr := invoke(t, "-bench", "B2"); !strings.Contains(stderr, "B2/k=10, B2/k=20, B2/k=40") {
		t.Errorf("missing parameters should list the choices: %q", stderr)
	}
}

func TestList(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"-list"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got, want := strings.Count(out.String(), "\n"), len(bench.Workloads()); got != want || !strings.Contains(out.String(), "B8/pad=256/k=40\n") {
		t.Errorf("-list printed %d lines, want %d:\n%s", got, want, out.String())
	}
}

// A wrong result must never look like a measurement: exit 1, nothing on
// stdout.
func TestWrongResultFails(t *testing.T) {
	defer func(f func(string) (bench.Workload, error)) { find = f }(find)
	find = func(name string) (bench.Workload, error) {
		w, err := bench.Find(name)
		w.Want.States++
		return w, err
	}
	code, kv, _, stderr := invoke(t, "-bench", "B1")
	if code != 1 || len(kv) != 0 || !strings.Contains(stderr, "states discovered (Stats.Admitted) 9, want 10") {
		t.Errorf("exit %d, stdout %v, stderr %q", code, kv, stderr)
	}
}

func TestProfiles(t *testing.T) {
	dir := t.TempDir()
	cpu, mem := filepath.Join(dir, "cpu.out"), filepath.Join(dir, "mem.out")
	code, kv, _, stderr := invoke(t, "-bench", "B1", "-cpuprofile", cpu, "-memprofile", mem)
	if code != 0 || kv["cpu-profile"] != cpu || kv["mem-profile"] != mem {
		t.Fatalf("exit %d, %v, %s", code, kv, stderr)
	}
	for _, p := range []string{cpu, mem} {
		if fi, err := os.Stat(p); err != nil || fi.Size() == 0 {
			t.Errorf("%s: %v", p, err)
		}
	}
	if code, _, _, stderr := invoke(t, "-bench", "B1", "-cpuprofile", filepath.Join(dir, "no", "such", "dir")); code != 1 || stderr == "" {
		t.Errorf("unwritable profile: exit %d, %q", code, stderr)
	}
}
