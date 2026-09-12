//go:build windows
// +build windows

package httpapi

import (
	"syscall"
	"unsafe"
)

var (
	getConsoleScreenBufferInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleScreenBufferInfo")
)

type COORD struct {
	X int16
	Y int16
}

type SMALL_RECT struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type CONSOLE_SCREEN_BUFFER_INFO struct {
	Size              COORD
	CursorPosition    COORD
	Attributes        uint16
	Window            SMALL_RECT
	MaximumWindowSize COORD
}

func terminalSize() (int, int) {
	var info CONSOLE_SCREEN_BUFFER_INFO
	handle, _ := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	ret, _, _ := getConsoleScreenBufferInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 80, 24
	}
	width := int(info.Window.Right - info.Window.Left + 1)
	height := int(info.Window.Bottom - info.Window.Top + 1)
	if width <= 0 || height <= 0 {
		return 80, 24
	}
	return width, height
}