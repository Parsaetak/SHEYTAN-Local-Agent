// Package sysinfo probes the host machine for CPU, RAM, GPU, VRAM, OS,
// and recommends llama.cpp runtime knobs (num_thread, num_ctx, num_gpu, etc.)
package sysinfo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

var _ = proc.Command // used below on windows/darwin probes

// probeTimeout is the HARD per-call bound for one external probe command
// (v1.2.5). A hung PowerShell/CIM/wmic call used to block the probe
// forever — and because Probe() is a sync.Once, every later caller
// (engine start GPU checks, /api/environment, /api/health) would block
// behind it too. Every probe is now bounded; a timed-out probe yields
// "unknown" facts, never a hang.
const probeTimeout = 8 * time.Second

// probeOutput runs one external probe with the bounded timeout.
func probeOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := proc.CommandContext(ctx, name, args...)

	return cmd.Output()
}

// SysInfo is the full hardware/software snapshot of the host.
type SysInfo struct {
	OS          string      `json:"os"`
	Arch        string      `json:"arch"`
	Hostname    string      `json:"hostname"`
	CPU         CPUInfo     `json:"cpu"`
	RAM         RAMInfo     `json:"ram"`
	Disk        DiskInfo    `json:"disk"`
	GPU         []GPUInfo   `json:"gpus"`
	WSL2        bool        `json:"wsl2"`
	Docker      bool        `json:"docker"`
	Recommended Recommended `json:"recommended"`
}

type CPUInfo struct {
	Name          string `json:"name"`
	PhysicalCores int    `json:"physicalCores"`
	LogicalCores  int    `json:"logicalCores"`
	FrequencyMHz  int    `json:"frequencyMHz"`
}

type RAMInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	Available  uint64 `json:"availableBytes"`
}

type DiskInfo struct {
	TotalBytes uint64 `json:"totalBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	Path       string `json:"path"`
}

type GPUInfo struct {
	Vendor    string `json:"vendor"`
	Name      string `json:"name"`
	VRAMBytes uint64 `json:"vramBytes"`
	DriverVer string `json:"driverVersion"`
}

type Recommended struct {
	NumThread int      `json:"numThread"`
	NumGPU    int      `json:"numGPU"`
	NumCtx    int      `json:"numCtx"`
	NumBatch  int      `json:"numBatch"`
	MaxTokens int      `json:"maxTokens"`
	CanRunCPU bool     `json:"canRunCPU"`
	CanRunGPU bool     `json:"canRunGPU"`
	Warnings  []string `json:"warnings"`
}

// Probe collects system information.
//
// v1.0.4: CACHED — the probe shells out to wmic/powershell on Windows, and
// the engine launcher + GPU-offload check + UI each asked for it on every
// start. Hardware does not change inside a session, so the first probe is
// reused for the process lifetime.
func Probe() *SysInfo {
	probeOnce.Do(func() {
		probeCache = probeUncached()
	})
	return probeCache
}

var (
	probeOnce  sync.Once
	probeCache *SysInfo
)

func probeUncached() *SysInfo {
	// v1.2.5: the first probe cost is MEASURED (brief §10) — one honest
	// duration log per process, so a slow Windows diagnostic is visible
	// instead of silent. Every external call inside is bounded by
	// probeTimeout, so the worst case is bounded, not unbounded.
	started := time.Now()

	info := &SysInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Hostname: hostname(),
		CPU:      probeCPU(),
		RAM:      probeRAM(),
	}
	// v1.2.2: a GPU-less or failed probe (WMI/PowerShell failures are
	// common under heavy inference load) must marshal as "gpus": [] -
	// never null, which crashed JSON consumers reading .length.
	info.GPU = probeGPUs()
	if info.GPU == nil {
		info.GPU = []GPUInfo{}
	}
	info.Disk = probeDisk(".")
	info.WSL2 = detectWSL2()
	info.Docker = detectDocker()
	info.Recommended = recommend(info)

	logging.Default().Info(
		"sysinfo",
		"hardware probe completed in %d ms (bounded: max %v per external call)",
		time.Since(started).Milliseconds(),
		probeTimeout,
	)

	return info
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func probeCPU() CPUInfo {
	c := CPUInfo{
		Name:          "Unknown",
		PhysicalCores: runtime.NumCPU(),
		LogicalCores:  runtime.NumCPU(),
	}
	switch runtime.GOOS {
	case "linux":
		c.Name = readFirstLine("/proc/cpuinfo", "model name")
		if coreSockets := readFirstLine("/proc/cpuinfo", "cpu cores"); coreSockets != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(coreSockets)); err == nil {
				c.PhysicalCores = n
			}
		}
		if freq := readFirstLine("/proc/cpuinfo", "cpu MHz"); freq != "" {
			if f, err := strconv.ParseFloat(strings.TrimSpace(freq), 64); err == nil {
				c.FrequencyMHz = int(f)
			}
		}
	case "darwin":
		if out, err := probeOutput("sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
			c.Name = strings.TrimSpace(string(out))
		}
		if out, err := probeOutput("sysctl", "-n", "hw.physicalcpu"); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
				c.PhysicalCores = n
			}
		}
		if out, err := probeOutput("sysctl", "-n", "hw.logicalcpu"); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
				c.LogicalCores = n
			}
		}
		if out, err := probeOutput("sysctl", "-n", "hw.cpufrequency"); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				c.FrequencyMHz = int(n / 1_000_000)
			}
		}
	case "windows":
		// v1.1.4Z: wmic is removed on Windows 11 24H2+. Query CIM via
		// PowerShell first, fall back to wmic on older builds where the
		// CIM class is unavailable. Both are cached for the process
		// lifetime (Probe is a sync.Once), so the extra cost is paid once.
		if v := cimScalar("Win32_Processor", "Name"); v != "" {
			c.Name = v
		} else if out, err := probeOutput("wmic", "cpu", "get", "name"); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				c.Name = strings.TrimSpace(lines[1])
			}
		}

		if v := cimScalar("Win32_Processor", "NumberOfCores"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				c.PhysicalCores = n
			}
		} else if out, err := probeOutput("wmic", "cpu", "get", "NumberOfCores"); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				if n, err := strconv.Atoi(strings.TrimSpace(lines[1])); err == nil {
					c.PhysicalCores = n
				}
			}
		}

		if v := cimScalar("Win32_Processor", "NumberOfLogicalProcessors"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				c.LogicalCores = n
			}
		}
	}
	return c
}

// cimScalar reads one property of the first instance of a CIM/WMI class
// through PowerShell (returns "" on any failure).
func cimScalar(class, property string) string {
	out, err := probeOutput(
		"powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-CimInstance -ClassName %s).%s", class, property),
	)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	v = strings.Trim(v, "\"")
	return v
}

func probeRAM() RAMInfo {
	r := RAMInfo{}
	switch runtime.GOOS {
	case "linux":
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.TrimSuffix(val, " kB")
				n, _ := strconv.ParseUint(val, 10, 64)
				switch key {
				case "MemTotal":
					r.TotalBytes = n * 1024
				case "MemFree":
					r.FreeBytes = n * 1024
				case "MemAvailable":
					r.Available = n * 1024
				}
			}
		}
	case "darwin":
		if out, err := probeOutput("sysctl", "-n", "hw.memsize"); err == nil {
			if n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
				r.TotalBytes = n
			}
		}
	case "windows":
		// v1.1.4Z: CIM first (wmic is gone on Windows 11 24H2+).
		if v := cimScalar("Win32_OperatingSystem", "TotalVisibleMemorySize"); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				r.TotalBytes = n * 1024
			}
		} else if out, err := probeOutput("wmic", "OS", "get", "TotalVisibleMemorySize"); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				if n, err := strconv.ParseUint(strings.TrimSpace(lines[1]), 10, 64); err == nil {
					r.TotalBytes = n * 1024
				}
			}
		}

		if v := cimScalar("Win32_OperatingSystem", "FreePhysicalMemory"); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				r.FreeBytes = n * 1024
			}
		}
	}
	return r
}

func probeDisk(path string) DiskInfo {
	d := DiskInfo{Path: path}
	abs, _ := filepath.Abs(path)
	d.Path = abs
	// Use 'df' on linux/darwin; wmic on windows
	if runtime.GOOS == "windows" {
		if out, err := probeOutput("wmic", "logicaldisk", "where", "DeviceID='"+filepath.VolumeName(abs)+"'", "get", "FreeSpace,Size"); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(strings.TrimSpace(lines[1]))
				if len(fields) == 2 {
					d.FreeBytes, _ = strconv.ParseUint(fields[0], 10, 64)
					d.TotalBytes, _ = strconv.ParseUint(fields[1], 10, 64)
				}
			}
		}
	} else {
		if out, err := probeOutput("df", "-k", abs); err == nil {
			lines := strings.Split(string(out), "\n")
			if len(lines) > 1 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 4 {
					total, _ := strconv.ParseUint(fields[1], 10, 64)
					free, _ := strconv.ParseUint(fields[3], 10, 64)
					d.TotalBytes = total * 1024
					d.FreeBytes = free * 1024
				}
			}
		}
	}
	return d
}

func probeGPUs() []GPUInfo {
	var gpus []GPUInfo
	// NVIDIA via nvidia-smi
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		if out, err := probeOutput("nvidia-smi",
			"--query-gpu=name,driver_version,memory.total",
			"--format=csv,noheader,nounits"); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				fields := strings.Split(line, ", ")
				if len(fields) < 3 {
					continue
				}
				vramMB, _ := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
				gpus = append(gpus, GPUInfo{
					Vendor:    "NVIDIA",
					Name:      strings.TrimSpace(fields[0]),
					DriverVer: strings.TrimSpace(fields[1]),
					VRAMBytes: vramMB * 1024 * 1024,
				})
			}
		}
	}
	// Apple Metal via system_profiler
	if runtime.GOOS == "darwin" {
		if out, err := probeOutput("system_profiler", "SPDisplaysDataType"); err == nil {
			text := string(out)
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "Chipset Model") {
					gpus = append(gpus, GPUInfo{
						Vendor: "Apple",
						Name:   strings.TrimSpace(strings.SplitN(line, ":", 2)[1]),
					})
				}
				if strings.Contains(line, "VRAM") && len(gpus) > 0 {
					v := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
					if strings.HasSuffix(v, " MB") {
						if n, err := strconv.ParseUint(strings.TrimSuffix(v, " MB"), 10, 64); err == nil {
							gpus[len(gpus)-1].VRAMBytes = n * 1024 * 1024
						}
					}
				}
			}
		}
	}
	// v1.0.3: Windows — every GPU (NVIDIA, AMD, Intel) via WMI. This matters
	// because the bundled engine uses Vulkan, which accelerates ALL of them;
	// the old nvidia-smi-only probe hid AMD/Intel GPUs and disabled offload.
	if runtime.GOOS == "windows" && len(gpus) == 0 {
		gpus = append(gpus, probeGPUsWindows()...)
	}
	return gpus
}

// probeGPUsWindows lists video controllers through the WMI
// Win32_VideoController class (PowerShell ships with every Windows 10/11).
// AdapterRAM is a uint32 that saturates at ~4 GB — good enough as a
// presence/detection signal; VRAM sizes at or above the cap are reported
// as 4 GB.
func probeGPUsWindows() []GPUInfo {
	out, err := probeOutput("powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.Name)|$($_.AdapterRAM)" }`)
	if err != nil {
		return nil
	}
	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		name := strings.TrimSpace(parts[0])
		// Skip non-hardware display adapters (e.g. remote-session devices).
		lower := strings.ToLower(name)
		if strings.Contains(lower, "basic display") || strings.Contains(lower, "remote display") ||
			strings.Contains(lower, "paravirtual") || strings.Contains(lower, "virtual display") {
			continue
		}
		g := GPUInfo{Name: name}
		if len(parts) == 2 {
			if ram, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
				g.VRAMBytes = ram
			}
		}
		switch {
		case strings.Contains(lower, "nvidia") || strings.Contains(lower, "geforce") || strings.Contains(lower, "quadro"):
			g.Vendor = "NVIDIA"
		case strings.Contains(lower, "amd") || strings.Contains(lower, "radeon"):
			g.Vendor = "AMD"
		case strings.Contains(lower, "intel") || strings.Contains(lower, "arc") || strings.Contains(lower, "iris"):
			g.Vendor = "Intel"
		default:
			g.Vendor = "Unknown"
		}
		gpus = append(gpus, g)
	}
	return gpus
}

func detectWSL2() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

func detectDocker() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

func readFirstLine(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// recommend derives llama.cpp runtime knobs from the host's hardware.
func recommend(info *SysInfo) Recommended {
	r := Recommended{
		NumThread: info.CPU.PhysicalCores,
		NumGPU:    0,
		NumCtx:    8192,
		NumBatch:  512,
		MaxTokens: 1024,
		CanRunCPU: info.RAM.TotalBytes >= 4*1024*1024*1024,
		CanRunGPU: false,
	}
	for _, gpu := range info.GPU {
		if gpu.VRAMBytes >= 2*1024*1024*1024 {
			r.CanRunGPU = true
			r.NumGPU = 99 // offload all layers
			break
		}
	}
	// Scale context window to RAM
	if info.RAM.TotalBytes >= 32*1024*1024*1024 {
		r.NumCtx = 16384
	} else if info.RAM.TotalBytes >= 16*1024*1024*1024 {
		r.NumCtx = 8192
	} else if info.RAM.TotalBytes >= 8*1024*1024*1024 {
		r.NumCtx = 4096
	}
	// Warnings
	if !r.CanRunCPU {
		r.Warnings = append(r.Warnings, "Insufficient RAM (<4 GB) — CPU inference will fail")
	}
	if r.NumThread < 4 {
		r.Warnings = append(r.Warnings, "Few CPU cores — expect slow CPU inference")
	}
	if !r.CanRunGPU && len(info.GPU) == 0 {
		r.Warnings = append(r.Warnings, "No GPU detected — falling back to CPU")
	}
	return r
}

// RecommendThreads returns (generationThreads, prefillThreads):
// token generation prefers PHYSICAL cores (SMT siblings fight for the
// same execution units and usually cost 5-15% tok/s), while prompt
// prefill parallelizes well across every LOGICAL core. This is the llama.cpp
// tuning consensus as of 2026 (v1.0.4 Speed Pack).
func RecommendThreads() (gen int, batch int) {
	info := Probe()
	gen = info.CPU.PhysicalCores
	batch = info.CPU.LogicalCores
	if gen <= 0 {
		gen = runtime.NumCPU()
	}
	if batch <= 0 {
		batch = runtime.NumCPU()
	}
	if gen > batch {
		gen = batch // defensive: never claim more gen threads than logical cores
	}
	return gen, batch
}

// FormatBytes pretty-prints byte counts.
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
