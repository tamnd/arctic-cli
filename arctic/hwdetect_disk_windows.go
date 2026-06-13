//go:build windows

package arctic

import (
	"syscall"
	"unsafe"
)

// diskFreeGB calls GetDiskFreeSpaceExW so the budget can size itself on Windows
// without a cgo dependency.
func diskFreeGB(path string) float64 {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var freeBytesAvailable uint64
	r, _, _ := proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		0, 0,
	)
	if r == 0 {
		return 0
	}
	return float64(freeBytesAvailable) / gb
}
