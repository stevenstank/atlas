// Command atlasbench runs one benchmark workload once, in its own process,
// and prints its environment, semantic result, timing, and memory
// (docs/BENCHMARKS.md §5). It complements, and does not replace, the
// testing.B suite in internal/bench.
//
// Usage:
//
//	atlasbench -list
//	atlasbench -plan -seed N [-rounds R]
//	atlasbench -bench NAME [-format kv|bench] [-cpuprofile FILE] [-memprofile FILE]
//
// NAME is a workload name from -list, such as B1, B2/k=10, B7, or V1. Exit
// status is 0 on success, 1 if the run failed or its result was wrong, and 2
// for a usage error. A wrong result prints nothing on stdout.
//
// -plan prints the run order of a baseline campaign (bench.Plan,
// BENCHMARKS.md §5) for master seed N (canonical decimal, bench.ParseSeed)
// and R rounds per set (default 10), as
// tab-separated lines under the header bench.PlanHeader. It measures nothing.
//
// With -format kv (the default) the output is key=value lines: the
// environment keys of bench.CaptureEnv; then
//
//	driver-version, benchmark, param-<name> for each parameter,
//	cpu-profile, mem-profile (a path, or "off"),
//	status, states, transitions, invariant, trace-length (NA if none),
//	wall-time-ns, time-to-first-counterexample-ns, violation-detection-ns
//	(both NA unless Violation), peak-rss-bytes (NA off Linux), alloc-bytes,
//	allocs, gc-cycles, gc-cpu-fraction, replay (ok, or NA if none).
//
// With -format bench the environment is printed as Go benchmark
// configuration lines and the measurements as one benchmark result line,
// "BenchmarkSuite/NAME 1 <wall> ns/op ...", which benchstat reads. ns/op is
// wall-time-ns, so for a Violation it is the time to first counterexample;
// violation-detection-ns appears only for Violation workloads.
//
// wall-time-ns is the duration of the check.Run call alone: it excludes flag
// parsing, environment capture, profile setup and writing, memory-statistics
// reads, the result check, the replay, and output.
//
// time-to-first-counterexample-ns is the BENCHMARKS.md §2 metric: from
// immediately before check.Run until it returns the Violation, including
// trace reconstruction. It is the same interval as wall-time-ns.
//
// violation-detection-ns is from immediately before check.Run until the
// first invariant evaluation that returns false. The engine evaluates
// invariants once per state, when the state is admitted, and stops at the
// first failure, so this is when the violating state is found, before the
// trace is rebuilt. It is taken by wrapping the invariants, not by changing
// the engine; the wrapper is used only for Violation workloads. It is not the
// suite's "time to first counterexample".
//
// peak-rss-bytes is the kernel's peak resident set size of the whole process
// (ru_maxrss), read right after check.Run returns, so it excludes the replay
// and the profile writing but includes runtime startup and, when enabled,
// the CPU profiler. It is resident memory, not Go heap. /usr/bin/time -v
// reports the same counter over the whole process lifetime.
//
// alloc-bytes, allocs, and gc-cycles are the changes in runtime.MemStats
// TotalAlloc, Mallocs, and NumGC across check.Run. gc-cpu-fraction is the
// change in /cpu/classes/gc/total:cpu-seconds over the change in
// /cpu/classes/total:cpu-seconds (runtime/metrics estimates).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"time"

	"github.com/stevenstank/atlas/check"
	"github.com/stevenstank/atlas/internal/bench"
)

const driverVersion = "2"

// find resolves a workload name; tests replace it.
var find = bench.Find

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// counters reads the allocation, GC, and CPU-class counters.
func counters() (alloc, mallocs uint64, gcs uint32, gcCPU, totalCPU float64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/total:cpu-seconds"}}
	metrics.Read(s)
	return ms.TotalAlloc, ms.Mallocs, ms.NumGC, s[0].Value.Float64(), s[1].Value.Float64()
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("atlasbench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("bench", "", "workload name, e.g. B2/k=10 (see -list)")
	list := fs.Bool("list", false, "list workload names and exit")
	plan := fs.Bool("plan", false, "print a baseline campaign's run order and exit")
	seedText := fs.String("seed", "", "master seed for -plan: a canonical decimal integer (bench.ParseSeed)")
	rounds := fs.Int("rounds", 10, "rounds per set for -plan")
	format := fs.String("format", "kv", "output format: kv or bench")
	cpuPath := fs.String("cpuprofile", "", "write a CPU profile of the check.Run call to `file`")
	memPath := fs.String("memprofile", "", "write a heap profile after check.Run to `file`")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 || (*format != "kv" && *format != "bench") {
		fmt.Fprintf(stderr, "atlasbench: unexpected arguments %q or -format %q\n", fs.Args(), *format)
		return 2
	}
	if *plan {
		seed, err := bench.ParseSeed(*seedText)
		if err != nil || *rounds < 1 || *list || *name != "" {
			fmt.Fprintln(stderr, "atlasbench: -plan needs a canonical decimal -seed, a positive -rounds, and no -list or -bench")
			return 2
		}
		var names []string
		for _, w := range bench.Workloads() {
			names = append(names, w.Name())
		}
		fmt.Fprintln(stdout, bench.PlanHeader)
		for _, e := range bench.Plan(names, *rounds, seed) {
			fmt.Fprintln(stdout, e)
		}
		return 0
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

	env := bench.CaptureEnv()
	head := [][2]string{{"driver-version", driverVersion}, {"benchmark", w.Name()}}
	for _, p := range w.Params() {
		head = append(head, [2]string{"param-" + p[0], p[1]})
	}
	head = append(head, [2]string{"cpu-profile", or(*cpuPath, "off")}, [2]string{"mem-profile", or(*memPath, "off")})

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
	detected := time.Duration(-1)
	var start time.Time
	if w.Want.Status == check.Violation {
		hook = func() { detected = time.Since(start) }
	}
	alloc0, mallocs0, gc0, gcCPU0, cpu0 := counters()
	start = time.Now()
	o := w.Run(context.Background(), hook)
	wall := time.Since(start)
	alloc1, mallocs1, gc1, gcCPU1, cpu1 := counters()

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
	violation := w.Want.Status == check.Violation
	if violation && detected < 0 {
		fmt.Fprintf(stderr, "atlasbench: %s: violation found but the invariant hook never fired\n", w.Name())
		return 1
	}

	trace, ttfc, detect := "NA", "NA", "NA"
	if o.TraceLen >= 0 {
		trace = strconv.Itoa(o.TraceLen)
	}
	if violation {
		ttfc = strconv.FormatInt(wall.Nanoseconds(), 10)
		detect = strconv.FormatInt(detected.Nanoseconds(), 10)
	}
	gcFrac := "NA"
	if d := cpu1 - cpu0; d > 0 {
		gcFrac = strconv.FormatFloat((gcCPU1-gcCPU0)/d, 'f', 4, 64)
	}
	u := func(v uint64) string { return strconv.FormatUint(v, 10) }
	result := [][2]string{
		{"status", o.Status.String()},
		{"states", strconv.FormatInt(o.States, 10)},
		{"transitions", strconv.FormatInt(o.Transitions, 10)},
		{"invariant", or(o.Invariant, "NA")},
		{"trace-length", trace},
		{"wall-time-ns", strconv.FormatInt(wall.Nanoseconds(), 10)},
		{"time-to-first-counterexample-ns", ttfc},
		{"violation-detection-ns", detect},
		{"peak-rss-bytes", rss},
		{"alloc-bytes", u(alloc1 - alloc0)},
		{"allocs", u(mallocs1 - mallocs0)},
		{"gc-cycles", u(uint64(gc1 - gc0))},
		{"gc-cpu-fraction", gcFrac},
		{"replay", replay},
	}

	if *format == "bench" {
		// Only the environment goes in configuration lines: they carry over
		// to later results in the same file, so per-workload keys would leak.
		fmt.Fprint(stdout, env)
		fmt.Fprintf(stdout, "BenchmarkSuite/%s 1 %d ns/op", w.Name(), wall.Nanoseconds())
		for _, m := range [][2]string{{rss, "peak-rss-bytes"}, {u(alloc1 - alloc0), "B/op"}, {u(mallocs1 - mallocs0), "allocs/op"},
			{u(uint64(gc1 - gc0)), "gc-cycles"}, {gcFrac, "gc-cpu-fraction"}, {detect, "violation-detection-ns"}} {
			if m[0] != "NA" {
				fmt.Fprintf(stdout, " %s %s", m[0], m[1])
			}
		}
		fmt.Fprintln(stdout)
		return 0
	}
	for _, kv := range append(append([][2]string(env), head...), result...) {
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
