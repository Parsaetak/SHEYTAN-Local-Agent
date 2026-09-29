//go:build !linux

// cpu_other.go — v1.8.0 CPU load seam (non-Linux platforms).
//
// Windows/me: the codebase's CPU measurement authority is the CIM probe
// (internal/sysinfo), which is a STARTUP/identity probe, not a live load
// signal — polling it from the Governor would duplicate an authority and
// burn the budget it protects. Until a live Windows load authority exists,
// the CPU seam reports "not measurable" and every CPU policy branch stays
// OFF. The self-model reports CPU as unknown — honestly.
package governor

// CPULoadPlatform reports that CPU load is not measurable on this
// platform for live policy purposes.
func CPULoadPlatform() (float64, bool) {
	return 0, false
}
