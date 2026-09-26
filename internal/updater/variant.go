// variant.go — v1.6.1: backend-variant engine provisioning (Vulkan must
// be real, not cosmetic).
//
// THE v1.6.0 GAP: the engine provisioning path always downloaded the
// CPU-only llama.cpp Windows package (llama-<tag>-bin-win-cpu-x64.zip).
// The accelerator resolver correctly refuses to claim GPU_VULKAN without
// runtime evidence — but with a CPU-only engine installed, Vulkan
// offload can NEVER happen: the Vulkan backend DLL is never even on
// disk. The GPU profile was therefore cosmetic: selectable, honestly
// unverified, and permanently unsatisfiable.
//
// This file makes the variant a first-class provisioning concept:
//
//   - AssetVariant names the backend family an engine package carries
//     ("cpu", "vulkan");
//   - AssetNameForVariant maps (tag, variant, platform) to the REAL
//     upstream asset (llama.cpp publishes Windows Vulkan builds as
//     llama-<tag>-bin-win-vulkan-x64.zip);
//   - the install manifest records the variant, so the installed
//     package's backend identity is inspectable (a v1.6.0 manifest with
//     no variant field reads back as "cpu");
//   - ResolveDownloadURLForVariant / EnsureEngineVariant reuse the SAME
//     transactional installer, downloader and lease authorities as the
//     CPU path — there is no second installer;
//   - an explicit VULKAN request that cannot be satisfied (no upstream
//     asset for this platform/tag) fails LOUDLY with an actionable
//     error — it never silently installs the CPU package instead.
//     AUTO keeps the evidence-gated discipline: CPU stays the safe
//     fallback exactly where the policy permits it.
package updater

import (
        "context"
        "fmt"
        "net/http"
        "runtime"
        "strings"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// AssetVariant is the backend family an engine package carries.
type AssetVariant string

const (
        // VariantCPU is the portable CPU-only engine package.
        VariantCPU AssetVariant = "cpu"

        // VariantVulkan is the llama.cpp Vulkan backend package (Windows
        // prebuilt: llama-<tag>-bin-win-vulkan-x64.zip). A Vulkan package
        // still runs on CPU when no device is usable — the Vulkan backend is
        // additive — but only a Vulkan package can ever satisfy GPU_VULKAN.
        VariantVulkan AssetVariant = "vulkan"
)

// NormalizeAssetVariant canonicalizes a variant string ("" and unknown
// values read as the historical CPU default).
func NormalizeAssetVariant(s string) AssetVariant {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case "vulkan", "gpu_vulkan", "gpu-vulkan":
                return VariantVulkan
        default:
                return VariantCPU
        }
}

// SupportedVariants lists the variants this platform can provision (in
// preference order). Non-Windows hosts: CPU only — llama.cpp publishes
// no Vulkan prebuilt for them, and claiming otherwise would be cosmetic.
func SupportedVariants() []AssetVariant {
        if runtime.GOOS == "windows" {
                return []AssetVariant{VariantCPU, VariantVulkan}
        }
        return []AssetVariant{VariantCPU}
}

// VariantSupported reports whether this platform can provision variant.
func VariantSupported(v AssetVariant) bool {
        for _, s := range SupportedVariants() {
                if s == v {
                        return true
                }
        }
        return false
}

// AssetNameForVariant returns the prebuilt llama.cpp asset name for this
// OS/arch at the given tag carrying the requested backend variant, or ""
// when no prebuilt asset exists (e.g. Vulkan outside Windows).
func AssetNameForVariant(tag string, v AssetVariant) string {
        var arch string
        switch runtime.GOARCH {
        case "amd64":
                arch = "x64"
        case "arm64":
                arch = "arm64"
        default:
                return ""
        }

        v = NormalizeAssetVariant(string(v))

        switch runtime.GOOS {
        case "windows":
                if v == VariantVulkan {
                        return fmt.Sprintf("llama-%s-bin-win-vulkan-%s.zip", tag, arch)
                }
                return fmt.Sprintf("llama-%s-bin-win-cpu-%s.zip", tag, arch)
        case "darwin":
                if v == VariantVulkan {
                        return "" // no Vulkan prebuilt upstream — never claim one
                }
                return fmt.Sprintf("llama-%s-bin-macos-%s.zip", tag, arch)
        case "linux":
                if v == VariantVulkan {
                        return "" // no Vulkan prebuilt upstream — never claim one
                }
                return fmt.Sprintf("llama-%s-bin-ubuntu-%s.zip", tag, arch)
        }
        return ""
}

// AssetURLForVariant builds the download URL for the variant asset at a
// tag ("" when the platform has no such prebuilt).
func AssetURLForVariant(tag string, v AssetVariant) string {
        a := AssetNameForVariant(tag, v)
        if a == "" {
                return ""
        }
        return "https://github.com/ggml-org/llama.cpp/releases/download/" + tag + "/" + a
}

// VariantExists HEAD-checks the variant asset at the given tag.
func VariantExists(ctx context.Context, tag string, v AssetVariant) bool {
        url := AssetURLForVariant(tag, v)
        if url == "" {
                return false
        }

        checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()

        req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
        if err != nil {
                return false
        }

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return false
        }
        _ = resp.Body.Close()

        return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// ResolveDownloadURLForVariant resolves (url, tag) for a fresh engine
// download carrying the requested backend variant: the pinned tag when
// its variant asset exists, else the newest release that carries it.
// An explicit variant request that cannot be satisfied returns an
// actionable error — it NEVER silently resolves to the CPU package.
func ResolveDownloadURLForVariant(ctx context.Context, v AssetVariant) (string, string, error) {
        v = NormalizeAssetVariant(string(v))

        if !VariantSupported(v) {
                return "", "", fmt.Errorf(
                        "no prebuilt llama.cpp %s engine package exists for %s/%s — the %s backend variant is not served by upstream releases for this platform; use the CPU engine (it stays the safe fallback) or build a Vulkan llama-server from source and set llamaBinPath",
                        v, runtime.GOOS, runtime.GOARCH, v,
                )
        }

        if url := AssetURLForVariant(DefaultEngineTag, v); url != "" {
                probeCtx, probeCancel := context.WithTimeout(ctx, 30*time.Second)

                if VariantExists(probeCtx, DefaultEngineTag, v) {
                        probeCancel()
                        return url, DefaultEngineTag, nil
                }

                probeCancel()
        }

        scanCtx, scanCancel := context.WithTimeout(ctx, 60*time.Second)
        tag, tagErr := LatestTag(scanCtx)
        scanCancel()

        if tagErr == nil && tag != "" {
                if url := AssetURLForVariant(tag, v); url != "" {
                        return url, tag, nil
                }

                return "", "", fmt.Errorf(
                        "no prebuilt llama.cpp %s engine package found for %s/%s in the newest releases",
                        v, runtime.GOOS, runtime.GOARCH,
                )
        }

        return "", "", fmt.Errorf(
                "could not resolve a downloadable %s engine (network check failed: %v) — connect to the internet once, or place a prebuilt llama-server(.exe) into the managed bin directory",
                v, tagErr,
        )
}

// InstalledEngineVariant reads the committed install manifest's backend
// variant (a v1.6.0-era manifest without the field reads back as "cpu").
func InstalledEngineVariant(cfg *config.Config) AssetVariant {
        m, ok := ReadInstallManifest(cfg)
        if !ok {
                return VariantCPU
        }
        return NormalizeAssetVariant(string(m.Variant))
}

// EnsureEngineVariant provisions the requested backend variant through
// the SAME transactional installer as every engine update. A no-op when
// the committed package already carries the variant; an explicit,
// actionable error when the variant cannot be provisioned — never a
// silent CPU install.
//
// The caller owns the engine lifecycle (stop before, verify after) — the
// same contract as InstallStaged.
func EnsureEngineVariant(
        ctx context.Context,
        cfg *config.Config,
        v AssetVariant,
        onProgress func(downloader.Progress),
) error {
        v = NormalizeAssetVariant(string(v))

        if !VariantSupported(v) {
                return fmt.Errorf(
                        "engine variant %q cannot be provisioned on %s/%s — upstream publishes no prebuilt package (keep the CPU engine, or set llamaBinPath to a self-built %s llama-server)",
                        v, runtime.GOOS, runtime.GOARCH, v,
                )
        }

        if InstalledEngineVariant(cfg) == v {
                logging.Default().Info("updater", "engine package already carries the %s backend variant — no provisioning needed", v)
                return nil
        }

        // Prefer the CURRENT engine tag (same release, different backend
        // build); fall back to the variant-aware release resolution.
        tag := InstalledEngineTag(cfg)
        if tag == "" || !VariantExists(ctx, tag, v) {
                url, resolved, err := ResolveDownloadURLForVariant(ctx, v)
                if err != nil {
                        return err
                }
                _ = url
                tag = resolved
        }

        logging.Default().Info("updater",
                "provisioning the %s engine variant (tag %s) through the transactional installer", v, tag)

        staged, err := InstallStagedDeferredWithVariant(ctx, cfg, tag, v, onProgress)
        if err != nil {
                return fmt.Errorf("provision %s engine: %w", v, err)
        }

        staged.Commit()

        return nil
}
