//go:build windows

// sysinfo_fast_windows.go — in-process Windows fast facts (v1.2.6).
//
// GlobalMemoryStatusEx (via kernel32) and GetDiskFreeSpaceEx (x/sys) are
// Win32 API calls — sub-millisecond, NO PowerShell/wmic process spawn.
// This is what makes the fast snapshot instant on Windows while the
// (single, batched) CIM probe runs in the background.
package sysinfo

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX layout (the package does
// not export it; the ABI is stable since forever).
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

func fastRAM() RAMInfo {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))

	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return RAMInfo{}
	}

	return RAMInfo{
		TotalBytes: status.TotalPhys,
		FreeBytes:  status.AvailPhys,
		Available:  status.AvailPhys,
	}
}

func fastDisk(path string) DiskInfo {
	d := DiskInfo{Path: path}

	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return d
	}

	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p16, &free, &total, &totalFree); err == nil {
		d.FreeBytes = free
		d.TotalBytes = total
	}

	return d
}
