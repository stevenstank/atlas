// Command atlasbench runs one benchmark workload once, in its own process,
// and prints its environment, semantic result, timing, and peak memory as
// key=value lines (docs/BENCHMARKS.md §5). It complements, and does not
// replace, the testing.B suite in internal/bench.
//
// Usage:
//
//	atlasbench -list
//	atlasbench -bench NAME [-cpuprofile FILE] [-memprofile FILE]
//
// NAME is a workload name from -list, such as B1, B2/k=10, or B7. Exit status
// is 0 on success, 1 if the run failed or its result was wrong, and 2 for a
// usage error. A wrong result prints nothing on stdout.
//
// Output keys, in order: the environment keys of bench.CaptureEnv; then
//
//	driver-version, benchmark, param-<name> for each parameter,
//	cpu-profile, mem-profile (a path, or "off"),
//	status, states, transitions, invariant, trace-length (NA if none),
//	wall-time-ns, time-to-first-counterexample-ns (NA for Exhausted),
//	peak-rss-bytes (NA off Linux), replay (ok, or NA if none).
//
// wall-time-ns is the duration of the check.Run call alone: it excludes flag
// parsing, environment capture, profile setup and writing, the result check,
// the replay, and output. For a Violation it includes rebuilding the trace,
// so it is the time until Run returns the counterexample.
//
// time-to-first-counterexample-ns is measured from just before check.Run to
// the first invariant evaluation that returns false. The engine evaluates
// invariants once per state, when the state is admitted, and stops at the
// first failure, so this is when the violating state is found, before the
// trace is rebuilt. It is taken by wrapping the invariants, not by changing
// the engine; the wrapper is used only for Violation workloads.
//
// peak-rss-bytes is the kernel's peak resident set size of the whole process
// (ru_maxrss), read right after check.Run returns, so it excludes the replay
// and the profile writing but includes runtime startup and, when enabled,
// the CPU profiler. It is resident memory, not Go heap. To cross-check it,
// run the binary under /usr/bin/time -v; that figure covers the whole
// process lifetime, including the replay.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/bench"
)

const driverVersion = "1"

// find resolves a workload name; tests replace it.
var find = bench.Find

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("atlasbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("bench", "", "workload name, e.g. B2/k=10 (see -list)")
	list := fs.Bool("list", false, "list workload names and exit")
	cpuPath := fs.String("cpuprofile", "", "write a CPU profile of the check.Run call to `file`")
	memPath := fs.String("memprofile", "", "write a heap profile after check.Run to `file`")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "atlasbench: unexpected arguments %q\n", fs.Args())
		return 2
	}
	if *list {
		for _, w := range bench.Workloads() {
			fmt.Fprintln(stdout, w.Name())
		}
		return 0
	}
	w, err := find(*name)
	if err != nil {
		fmt.Fprintln(stderr, "atlasbench:", err)
		return 2
	}

	out := [][2]string(bench.CaptureEnv())
	out = append(out, [2]string{"driver-version", driverVersion}, [2]string{"benchmark", w.Name()})
	for _, p := range w.Params() {
		out = append(out, [2]string{"param-" + p[0], p[1]})
	}
	out = append(out, [2]string{"cpu-profile", or(*cpuPath, "off")}, [2]string{"mem-profile", or(*memPath, "off")})

	var cpuFile, memFile *os.File
	for _, f := range []struct {
		path string
		file **os.File
	}{{*cpuPath, &cpuFile}, {*memPath, &memFile}} {
		if f.path == "" {
			continue
		}
		if *f.file, err = os.Create(f.path); err != nil {
			fmt.Fprintln(stderr, "atlasbench:", err)
			return 1
		}
		defer (*f.file).Close()
	}
	if cpuFile != nil {
		if err := pprof.StartCPUProfile(cpuFile); err != nil {
			fmt.Fprintln(stderr, "atlasbench: cpu profile:", err)
			return 1
		}
	}

	var hook func()
	found := time.Duration(-1)
	var start time.Time
	if w.Want.Status == check.Violation {
		hook = func() { found = time.Since(start) }
	}
	start = time.Now()
	o := w.Run(context.Background(), hook)
	wall := time.Since(start)

	if cpuFile != nil {
		pprof.StopCPUProfile()
	}
	rss := peakRSS()
	if memFile != nil {
		runtime.GC() // up-to-date heap statistics; after timing
		if err := pprof.WriteHeapProfile(memFile); err != nil {
			fmt.Fprintln(stderr, "atlasbench: heap profile:", err)
			return 1
		}
	}
	for _, f := range []*os.File{cpuFile, memFile} {
		if f != nil {
			if err := f.Close(); err != nil {
				fmt.Fprintln(stderr, "atlasbench:", err)
				return 1
			}
		}
	}

	if err := w.Want.Check(o.Summary); err != nil {
		fmt.Fprintf(stderr, "atlasbench: %s: %v\n", w.Name(), err)
		return 1
	}
	replay := "NA"
	if o.Replay != nil {
		if err := o.Replay(); err != nil {
			fmt.Fprintf(stderr, "atlasbench: %s: replay: %v\n", w.Name(), err)
			return 1
		}
		replay = "ok"
	}
	if w.Want.Status == check.Violation && found < 0 {
		fmt.Fprintf(stderr, "atlasbench: %s: violation found but the invariant hook never fired\n", w.Name())
		return 1
	}

	trace, ttfc := "NA", "NA"
	if o.TraceLen >= 0 {
		trace = strconv.Itoa(o.TraceLen)
	}
	if found >= 0 {
		ttfc = strconv.FormatInt(found.Nanoseconds(), 10)
	}
	out = append(out,
		[2]string{"status", o.Status.String()},
		[2]string{"states", strconv.FormatInt(o.States, 10)},
		[2]string{"transitions", strconv.FormatInt(o.Transitions, 10)},
		[2]string{"invariant", or(o.Invariant, "NA")},
		[2]string{"trace-length", trace},
		[2]string{"wall-time-ns", strconv.FormatInt(wall.Nanoseconds(), 10)},
		[2]string{"time-to-first-counterexample-ns", ttfc},
		[2]string{"peak-rss-bytes", rss},
		[2]string{"replay", replay},
	)
	for _, kv := range out {
		fmt.Fprintf(stdout, "%s=%s\n", kv[0], kv[1])
	}
	return 0
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
