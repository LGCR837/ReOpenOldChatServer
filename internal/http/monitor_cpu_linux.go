//go:build linux
// +build linux

package httpapi

import (
	"syscall"
	"time"
)

func readCPUTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	sec := time.Duration(ru.Utime.Sec+ru.Stime.Sec) * time.Second
	usec := time.Duration(ru.Utime.Usec+ru.Stime.Usec) * time.Microsecond
	return sec + usec
}