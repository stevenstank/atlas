//go:build linux

package main

import (
	"strconv"
	"syscall"
)

// peakRSS returns the kernel's high-water mark of this process's resident
// set size (getrusage RUSAGE_SELF, ru_maxrss, which Linux reports in KiB).
// It is not sampled: the kernel tracks the maximum itself.
func peakRSS() string {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return "NA"
	}
	return strconv.FormatInt(ru.Maxrss*1024, 10)
}
