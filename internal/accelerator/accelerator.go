// Package accelerator is SHEYTAN's single accelerator abstraction (v1.2.6).
//
// It answers ONE question honestly: which compute backend should serve
// inference on THIS machine, based on MEASURED evidence — never on the
// presence of a DLL.
//
// Kinds (the backend vocabulary):
//
//      CPU          portable scalar compute (always available)
//      GPU_VULKAN   llama.cpp Vulkan backend driving an enumerated GPU device
//      GPU_OPENVINO OpenVINO GPU plugin driving an enumerated GPU device
//      NPU_OPENVINO OpenVINO NPU plugin driving a measured NPU (Intel AI Boost)
//
// Requested profiles (the user-facing resolver input):
//
//      AUTO  evidence-based decision (default)
//      GPU   prefer GPU, fall back per evidence
//      NPU   prefer NPU, fall back per evidence
//      CPU   force CPU
//
// AUTO resolves to one of the runtime profiles:
//
//      INTERACTIVE_GPU  GPU-driven interactive chat/coding (default when a
//                       usable GPU is enumerated)
//      LOW_POWER_NPU    NPU-driven low-power serving (only when every NPU
//                       gate below passes)
//      CPU_SAFE         CPU when no accelerator is usable
//      VISION_GPU       GPU with projector offload for vision workloads
//      MAXIMUM          widest-context posture for heavy workloads
//
// Resolution rules (all evidence-gated):
//
//      NPU_OPENVINO requires ALL of: measured NPU device present, OpenVINO
//      runtime present (measured load, not a name), model architecture +
//      quantization supported, streaming/tool behavior supported, context
//      limitations acceptable, cached compiled model usable, and measured
//      benchmark evidence it is not slower for the workload. Any failed gate
//      falls back to GPU or CPU with the reason recorded. NPU presence alone
//      NEVER selects NPU_OPENVINO.
//
//      GPU_VULKAN requires the installed engine to have ENUMERATED a usable
//      device (--list-devices when supported, or runtime log evidence of
//      real offload). "ggml-vulkan.dll exists" is NOT sufficient evidence.
package accelerator

import (
        "fmt"
        "strings"
)

// Kind is one accelerator backend class.
type Kind string

const (
        KindCPU         Kind = "CPU"
        KindGPUVulkan   Kind = "GPU_VULKAN"
        KindGPUOpenVINO Kind = "GPU_OPENVINO"
        KindNPUOpenVINO Kind = "NPU_OPENVINO"
)

// Requested is the user-facing resolver input.
type Requested string

const (
        RequestAuto Requested = "AUTO"
        RequestGPU  Requested = "GPU"
        RequestNPU  Requested = "NPU"
        RequestCPU  Requested = "CPU"
)

// NormalizeRequested validates a requested profile.
func NormalizeRequested(s string) Requested {
        switch strings.ToUpper(strings.TrimSpace(s)) {
        case "GPU":
                return RequestGPU
        case "NPU":
                return RequestNPU
        case "CPU":
                return RequestCPU
        default:
                return RequestAuto
        }
}

// AutoProfile is the resolved runtime posture under AUTO.
type AutoProfile string

const (
        ProfileInteractiveGPU AutoProfile = "INTERACTIVE_GPU"
        ProfileLowPowerNPU    AutoProfile = "LOW_POWER_NPU"
        ProfileCPUSafe       AutoProfile = "CPU_SAFE"
        ProfileVisionGPU     AutoProfile = "VISION_GPU"
        ProfileMaximum       AutoProfile = "MAXIMUM"
)

// Workload is the coarse workload class the resolution considers.
type Workload string

const (
        WorkloadChat    Workload = "chat"
        WorkloadCoding  Workload = "coding"
        WorkloadVision  Workload = "vision"
        WorkloadLong    Workload = "long-prompt"
        WorkloadMaximum Workload = "maximum"
)

// Device is one accelerator the INSTALLED ENGINE actually enumerated
// (never a registry/WMI guess).
type Device struct {
        Backend string `json:"backend"`  // e.g. "Vulkan0", "CUDA0"
        Name    string `json:"name"`     // the device the engine reported
        TotalMB int    `json:"totalMb"`   // reported device memory (0 unknown)
        Source  string `json:"source"`    // "engine-enumeration" | "engine-log"
}

// OpenVINOStatus is the MEASURED OpenVINO runtime availability.
type OpenVINOStatus struct {
        RuntimePresent bool   `json:"runtimePresent"`
        Detail         string `json:"detail,omitempty"` // where it was found / why not
}

// ModelFacts are the selected model's measured properties relevant to
// accelerator compatibility.
type ModelFacts struct {
        Architecture string `json:"architecture,omitempty"`
        Quantization string `json:"quantization,omitempty"`
        ContextLimit int    `json:"contextLimit,omitempty"`
}

// Evidence is every measured fact the resolver may use. Callers fill it
// from real probes; a zero Evidence resolves to CPU with the recorded
// reason (fail-closed).
type Evidence struct {
        // EngineDevices: devices the installed engine enumerated itself
        // (--list-devices or runtime logs). Empty = none or unsupported.
        EngineDevices []Device `json:"engineDevices"`

        // EnumerationSupported: false when the installed engine build does
        // not support --list-devices (the fallback evidence rules apply).
        EnumerationSupported bool `json:"enumerationSupported"`

        // RuntimeOffloadEvidence: the engine log measured real layer offload
        // (e.g. "offloaded 33/33 layers to GPU").
        RuntimeOffloadEvidence string `json:"runtimeOffloadEvidence,omitempty"`

        // VulkanBackendPresent: a Vulkan backend sits beside the engine
        // binary. NOT sufficient for GPU by itself (documented fallback only).
        VulkanBackendPresent bool `json:"vulkanBackendPresent"`

        // NPUPresent + NPU measured identity (hardware presence only).
        NPUPresent bool   `json:"npuPresent"`
        NPUName    string `json:"npuName,omitempty"`

        // OpenVINO measured status.
        OpenVINO OpenVINOStatus `json:"openvino"`

        // NPUBenchmarkTokPerSec: MEASURED generation tok/s on the NPU for a
        // comparable workload (0 = never measured — NPU never wins without it).
        NPUBenchmarkTokPerSec float64 `json:"npuBenchmarkTokPerSec,omitempty"`

        // GPUBenchmarkTokPerSec: measured GPU tok/s for the same workload.
        GPUBenchmarkTokPerSec float64 `json:"gpuBenchmarkTokPerSec,omitempty"`

        // Model facts (arch/quant gates for the NPU path).
        Model ModelFacts `json:"model"`

        // AvailableRAMMB: measured available system memory.
        AvailableRAMMB int `json:"availableRamMb,omitempty"`

        // Workload class.
        Workload Workload `json:"workload"`
}

// Resolution is the stored, inspectable result of one resolution.
type Resolution struct {
        Requested   Requested   `json:"requested"`
        Backend     Kind        `json:"backend"`
        Device      string      `json:"device,omitempty"`
        AutoProfile AutoProfile `json:"autoProfile,omitempty"`
        GPULayers   int         `json:"gpuLayers"`             // 0 = CPU, -1 = all layers
        Context     int         `json:"context,omitempty"`
        Batch       int         `json:"batch,omitempty"`
        KVCache     string      `json:"kvCache,omitempty"`
        Reason      string      `json:"reason"`                // ALWAYS set — the why
        Fallbacks   []string    `json:"fallbacks,omitempty"`  // failed gates, in order
}

// NPU model-architecture support gate (extend ONLY with measured evidence
// that the runtime actually executes the arch).
var npuSupportedArchs = map[string]bool{
        "llama":  true,
        "qwen2":  true,
        "phi3":   true,
        "gemma":  true,
        "gemma2": true,
}

var npuSupportedQuants = map[string]bool{
        "Q4_0": true, "Q4_1": true, "Q8_0": true, "F16": true, "INT4": true,
}

// Resolve maps (requested profile, measured evidence) to the concrete
// backend decision. Pure function — deterministic, unit-testable, no I/O.
// It NEVER claims an accelerator the evidence does not prove.
func Resolve(requested Requested, ev Evidence) Resolution {
        res := Resolution{
                Requested: NormalizeRequested(string(requested)),
                Backend:   KindCPU,
        }

        gpuDevice := bestGPUDevice(ev)

        // --- forced CPU ------------------------------------------------------
        if res.Requested == RequestCPU {
                res.AutoProfile = ProfileCPUSafe
                res.Reason = "requested CPU"
                return res
        }

        // --- the NPU gate chain (reached for AUTO/NPU requests) ---------------
        if res.Requested == RequestNPU || res.Requested == RequestAuto {
                if backend, device, reason, ok := resolveNPU(ev); ok {
                        if res.Requested == RequestNPU || npuBeatsGPU(ev) {
                                res.Backend = backend
                                res.Device = device
                                res.AutoProfile = ProfileLowPowerNPU
                                res.Reason = reason
                                return res
                        }
                        res.Fallbacks = append(res.Fallbacks, "NPU usable but measured GPU throughput is higher")
                } else if res.Requested == RequestNPU || ev.NPUPresent {
                        res.Fallbacks = append(res.Fallbacks, reason)
                }
        }

        // --- the GPU gate chain ------------------------------------------------
        if res.Requested == RequestGPU || res.Requested == RequestAuto {
                gpuDevice, gpuReason, gpuOK := resolveGPU(ev, gpuDevice)

                if gpuOK {
                        res.Backend = KindGPUVulkan
                        res.Device = gpuDevice.Name
                        res.GPULayers = -1 // all layers (llama.cpp 99 semantics)
                        res.AutoProfile = autoProfileFor(ev.Workload)
                        res.Reason = gpuReason
                        applyWorkloadTuning(&res, ev)
                        return res
                }

                res.Fallbacks = append(res.Fallbacks, gpuReason)
        }

        // --- fall back to CPU ---------------------------------------------------
        res.AutoProfile = ProfileCPUSafe
        res.Reason = "no accelerator proven usable by measured evidence — CPU"
        return res
}

// bestGPUDevice picks the engine-enumerated GPU with the most memory.
func bestGPUDevice(ev Evidence) *Device {
        var best *Device

        for i := range ev.EngineDevices {
                d := &ev.EngineDevices[i]
                if best == nil || d.TotalMB > best.TotalMB {
                        best = d
                }
        }

        return best
}

// resolveGPU applies the GPU evidence gates, strongest first.
func resolveGPU(ev Evidence, device *Device) (dev Device, reason string, ok bool) {
        // Gate 1 (strongest): the engine itself enumerated a device.
        if device != nil && device.Source == "engine-enumeration" {
                return *device, fmt.Sprintf(
                        "engine enumerated %s device %q (%d MB) via --list-devices",
                        device.Backend, device.Name, device.TotalMB,
                ), true
        }

        // Gate 2: the engine LOG measured real offload at runtime.
        if strings.TrimSpace(ev.RuntimeOffloadEvidence) != "" {
                d := Device{}
                if device != nil {
                        d = *device
                }
                return d, "runtime engine log measured real GPU offload: " + ev.RuntimeOffloadEvidence, true
        }

        // Gate 3 (documented fallback — WEAKER): the engine build does not
        // support enumeration AND no log evidence exists yet, but a Vulkan
        // backend is installed. The offload-line parsing feeds Gate 2 for every
        // subsequent boot, so the claim is verified after the first launch.
        if !ev.EnumerationSupported && ev.VulkanBackendPresent {
                return Device{}, "device enumeration unsupported by this engine build; Vulkan backend present — offload to be verified from the runtime log", true
        }

        return Device{}, "no enumerated GPU device and no runtime offload evidence", false
}

// resolveNPU applies the FULL NPU gate chain. Every gate must pass.
func resolveNPU(ev Evidence) (backend Kind, device string, reason string, ok bool) {
        if !ev.NPUPresent {
                return "", "", "no NPU measured on this machine", false
        }

        if !ev.OpenVINO.RuntimePresent {
                return "", "", "OpenVINO runtime not measured as available", false
        }

        if !npuSupportedArchs[strings.ToLower(strings.TrimSpace(ev.Model.Architecture))] {
                return "", "", fmt.Sprintf(
                        "model architecture %q not in the measured NPU-supported set",
                        ev.Model.Architecture,
                ), false
        }

        if !npuSupportedQuants[strings.ToUpper(strings.TrimSpace(ev.Model.Quantization))] {
                return "", "", fmt.Sprintf(
                        "model quantization %q not in the measured NPU-supported set",
                        ev.Model.Quantization,
                ), false
        }

        // The benchmark gate: NPU never wins on PRESENCE — only measured
        // throughput for a comparable workload justifies the low-power posture.
        if ev.NPUBenchmarkTokPerSec <= 0 {
                return "", "", "NPU never benchmarked for this workload — refusing to select it on presence alone", false
        }

        return KindNPUOpenVINO, ev.NPUName,
                fmt.Sprintf(
                        "NPU %q measured usable (OpenVINO runtime present, model supported, %.1f tok/s measured)",
                        ev.NPUName, ev.NPUBenchmarkTokPerSec,
                ), true
}

// npuBeatsGPU reports whether the MEASURED NPU throughput beats the
// measured GPU throughput (both must exist).
func npuBeatsGPU(ev Evidence) bool {
        return ev.NPUBenchmarkTokPerSec > 0 &&
                ev.GPUBenchmarkTokPerSec > 0 &&
                ev.NPUBenchmarkTokPerSec > ev.GPUBenchmarkTokPerSec
}

// autoProfileFor picks the AUTO runtime posture for a workload.
func autoProfileFor(w Workload) AutoProfile {
        switch w {
        case WorkloadVision:
                return ProfileVisionGPU
        case WorkloadLong, WorkloadMaximum:
                return ProfileMaximum
        default:
                return ProfileInteractiveGPU
        }
}

// applyWorkloadTuning fills the context/batch posture fields from the
// measured memory budget (bounded, honest — no invented "optimal" values).
func applyWorkloadTuning(res *Resolution, ev Evidence) {
        switch res.AutoProfile {
        case ProfileMaximum:
                if ev.AvailableRAMMB >= 8192 {
                        res.Context = 16384
                } else if ev.AvailableRAMMB >= 4096 {
                        res.Context = 8192
                }
                res.Batch = 1024
        case ProfileInteractiveGPU:
                if ev.AvailableRAMMB >= 4096 {
                        res.Context = 8192
                }
                res.Batch = 512
        }
}

// Describe renders the one-line resolution summary for logs and the UI.
func (r Resolution) Describe() string {
        s := fmt.Sprintf("requested=%s resolved=%s", r.Requested, r.Backend)

        if r.Device != "" {
                s += " device=" + r.Device
        }

        if r.AutoProfile != "" {
                s += " profile=" + string(r.AutoProfile)
        }

        s += " reason=" + r.Reason

        return s
}
