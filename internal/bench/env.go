package bench

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Env is the benchmark environment (BENCHMARKS.md §5) as ordered key/value
// pairs. Keys are lowercase with no spaces, so String's output is made of
// configuration lines in the Go benchmark format, which benchstat reads
// alongside the results.
type Env [][2]string

// String renders one "key: value" line per entry.
func (e Env) String() string {
	var b strings.Builder
	for _, kv := range e {
		b.WriteString(kv[0] + ": " + kv[1] + "\n")
	}
	return b.String()
}

// Get returns the value for key, or "" if absent.
func (e Env) Get(key string) string {
	for _, kv := range e {
		if kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

// CaptureEnv records what BENCHMARKS.md §5 requires that the process can see.
// Values it cannot determine are "unknown". RAM, CPU model, kernel, and WSL
// detection read /proc and are "unknown" off Linux. The commit and dirty flag
// come from the binary's VCS stamp, or else from read-only git commands
// (go test binaries are not stamped). goos, goarch, and pkg are printed by
// go test itself and are repeated here for the future one-shot driver.
func CaptureEnv() Env {
	settings := map[string]string{}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			settings[s.Key] = s.Value
		}
	}
	or := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	kernel := strings.TrimSpace(readFile("/proc/sys/kernel/osrelease"))
	wsl := "no"
	if strings.Contains(strings.ToLower(kernel), "microsoft") || os.Getenv("WSL_DISTRO_NAME") != "" {
		wsl = "yes"
	}
	if kernel == "" {
		kernel, wsl = "unknown", "unknown"
	}
	commit, dirty := settings["vcs.revision"], settings["vcs.modified"]
	if commit == "" {
		commit = strings.TrimSpace(gitOutput("rev-parse", "HEAD"))
		if commit != "" {
			dirty = strconv.FormatBool(strings.TrimSpace(gitOutput("--no-optional-locks", "status", "--porcelain")) != "")
		}
	}
	buildFlags := []string{}
	for _, k := range []string{"-gcflags", "-ldflags", "-tags", "-race", "CGO_ENABLED"} {
		if v, ok := settings[k]; ok {
			buildFlags = append(buildFlags, k+"="+v)
		}
	}
	return Env{
		{"atlas-suite", SuiteVersion},
		{"atlas-commit", or(commit, "unknown")},
		{"atlas-dirty", or(dirty, "unknown")},
		{"go-version", runtime.Version()},
		{"goos", runtime.GOOS},
		{"goarch", runtime.GOARCH},
		{"goamd64", or(settings["GOAMD64"], or(os.Getenv("GOAMD64"), "unknown"))},
		{"build-flags", or(strings.Join(buildFlags, " "), "none recorded")},
		{"cpu-model", or(procField("/proc/cpuinfo", "model name"), "unknown")},
		{"num-cpu", strconv.Itoa(runtime.NumCPU())},
		{"gomaxprocs", strconv.Itoa(runtime.GOMAXPROCS(0))},
		{"gogc", or(os.Getenv("GOGC"), "default")},
		{"gomemlimit", or(os.Getenv("GOMEMLIMIT"), "default")},
		{"mem-total", or(procField("/proc/meminfo", "MemTotal"), "unknown")},
		{"kernel", kernel},
		{"wsl", wsl},
	}
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// procField returns the value of the first "name : value" line in a /proc
// file.
func procField(path, name string) string {
	sc := bufio.NewScanner(strings.NewReader(readFile(path)))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(k) == name {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// gitOutput runs a read-only git command; it returns "" on any failure.
func gitOutput(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}
