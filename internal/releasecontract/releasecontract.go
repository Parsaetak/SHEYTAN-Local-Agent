// Package releasecontract is the SINGLE authoritative definition of the
// release artifact naming for SHEYTAN-LA.
//
// v1.2.0 renamed the Windows portable artifacts from the legacy
// "SHEYTAN-Local-Agent-Windows-x64-v<ver>Z.zip" identity to
// "SHEYTAN-LA-v<ver>-windows-x64.zip". Two contracts had since drifted
// apart (the workflow vs the stress gate): the stress test kept failing
// on a name nothing produces any more.
//
// The rule now: the workflow's packaging steps, the NSIS installer build
// and the stress/release-surface verification all derive their names from
// this one helper. When a future release renames artifacts again, the
// contract changes HERE — and the stress gate detects any workflow that
// still emits (or stopped emitting) the contracted names, instead of
// silently pinning a second copy of the truth.
//
// The GitHub Actions workflow cannot import Go, so the stress gate acts as
// the enforcement arm: it extracts the artifact names the workflow ACTUALLY
// produces (generic `dist/…` regexes) and compares them against this
// contract with the version substituted. Any future rename in the workflow
// without a matching contract update fails the stress suite, and vice
// versa — naming drift is detected in both directions.
package releasecontract

import (
	"fmt"
	"strings"
)

// App roots and executable identity. The Windows app root follows the
// v1.2.0 short identity (SHEYTAN-LA); the Linux portable root keeps the
// long historical name because the Linux package layout predates it and
// the rename is a Windows-only contract change.
const (
	WindowsAppRoot = "SHEYTAN-LA"
	LinuxAppRoot   = "SHEYTAN-Local-Agent"
	WindowsExeName = "SHEYTAN-LA.exe"
	LinuxExeName   = "SHEYTAN-Local-Agent"
	LauncherScript = "SHEYTAN-LA.bat"
)

// Contract is the resolved artifact-name set for one version.
type Contract struct {
	Version string

	// Portable ZIPs.
	WindowsZip string // SHEYTAN-LA-v1.2.0-windows-x64.zip
	LinuxZip   string // SHEYTAN-Local-Agent-Linux-x64-v1.2.0Z.zip

	// Windows NSIS installer.
	WindowsInstaller string // SHEYTAN-LA-v1.2.0-windows-x64-installer.exe

	// Workflow placeholders: the exact spellings used inside
	// build-desktop.yml, with the version slot left to the workflow env.
	WindowsZipWorkflowSlot string // SHEYTAN-LA-v${env:APP_VERSION}-windows-x64.zip
	WindowsInstallerSlot   string // SHEYTAN-LA-v${env:APP_VERSION}-windows-x64-installer.exe
	LinuxZipWorkflowSlot   string // SHEYTAN-Local-Agent-Linux-x64-v${APP_VERSION}Z.zip
}

// For resolves the artifact-name contract for a semantic version (with or
// without a leading "v").
func For(version string) Contract {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")

	return Contract{
		Version: v,

		WindowsZip:       fmt.Sprintf("SHEYTAN-LA-v%s-windows-x64.zip", v),
		LinuxZip:         fmt.Sprintf("SHEYTAN-Local-Agent-Linux-x64-v%sZ.zip", v),
		WindowsInstaller: fmt.Sprintf("SHEYTAN-LA-v%s-windows-x64-installer.exe", v),

		WindowsZipWorkflowSlot: "SHEYTAN-LA-v${env:APP_VERSION}-windows-x64.zip",
		WindowsInstallerSlot:   "SHEYTAN-LA-v${env:APP_VERSION}-windows-x64-installer.exe",
		LinuxZipWorkflowSlot:   "SHEYTAN-Local-Agent-Linux-x64-v${APP_VERSION}Z.zip",
	}
}

// WindowsAppDir is the staging directory (relative to the repository root)
// the workflow assembles the portable Windows application into.
const WindowsAppDir = "dist/windows/app/" + WindowsAppRoot

// RequiredWindowsZipEntries lists the entries the Windows portable ZIP
// must contain, expressed against the contracted app root. The workflow's
// "Verify Windows ZIP" step checks the same list — one contract, two
// enforcement points.
func (c Contract) RequiredWindowsZipEntries() []string {
	return []string{
		WindowsAppRoot + "/" + WindowsExeName,
		WindowsAppRoot + "/README.txt",
		WindowsAppRoot + "/BUILD-INFO.txt",
		WindowsAppRoot + "/models/README.txt",
		WindowsAppRoot + "/workspace/README.txt",
	}
}

// RequiredLinuxZipEntries lists the entries the Linux portable ZIP
// verification step must check for.
func (c Contract) RequiredLinuxZipEntries() []string {
	return []string{
		LinuxAppRoot + "/" + LinuxExeName,
		WindowsAppRoot + "/README.txt",
		WindowsAppRoot + "/BUILD-INFO.txt",
	}
}
