//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procShellExecuteW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")

// openInBrowser opens a URL in the default browser through the shell,
// which hands it to whatever the user has set one up with.
func openInBrowser(url string) error {
	u16, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	const swShow = 1
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(u16)), 0, 0, swShow)
	// ShellExecuteW returns a value over 32 on success, and an error code
	// of the SE_ERR family otherwise.
	if r <= 32 {
		return fmt.Errorf("打开 %s 失败（ShellExecute %d）", url, r)
	}
	return nil
}
