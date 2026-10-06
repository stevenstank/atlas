//go:build !linux

package main

// peakRSS is not implemented off Linux; use an external tool such as
// /usr/bin/time.
func peakRSS() string { return "NA" }
