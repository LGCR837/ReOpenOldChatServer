//go:build windows
// +build windows

package httpapi

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getProcessTimes    = kernel32.NewProc("GetProcessTimes")
	queryPerformanceCounter = kernel32.NewProc("QueryPerformanceCounter")
	queryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
)

type FILETIME struct {
	DwLowDateTime  uint32
	DwHighDateTime int32
}

func readCPUTime() time.Duration {
	var creationTime, exitTime, kernelTime, userTime FILETIME
	handle, _ := syscall.GetCurrentProcess()
	ret, _, _ := getProcessTimes.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&creationTime)),
		uintptr(unsafe.Pointer(&exitTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return 0
	}
	kernelNanos := filetimeToNanos(kernelTime)
	userNanos := filetimeToNanos(userTime)
	return time.Duration(kernelNanos + userNanos)
}

func filetimeToNanos(ft FILETIME) int64 {
	return int64(ft.DwHighDateTime)<<32 | int64(ft.DwLowDateTime)
}