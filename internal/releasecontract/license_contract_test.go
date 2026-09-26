// license_contract_test.go — v1.7.1: the deterministic license-file
// hygiene contract.
//
// Section 6 of the v1.7.1 task consolidated the human-facing licensing
// story into ONE Markdown entry point (LICENSE.md) on top of the
// existing legal authorities (LICENSE, LICENSE-APACHE,
// LICENSE-PROPRIETARY, LICENSE-MAP.md, NOTICE.md). This test prevents
// accidental reintroduction of redundant license Markdown files (the
// LICENSE-2.md / LICENSE.old.md / LICENCE spelling drift class) while
// pinning the authoritative set so the cleanup cannot silently regress:
//
//   - the authoritative license file set is EXACT;
//   - the human-facing entry point LICENSE.md exists and references every
//     authority;
//   - no new license-named file (case-insensitive, any spelling) may
//     appear anywhere in the repository without changing THIS test —
//     which is the documented, deliberate friction.
package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from THIS package's location
// (internal/releasecontract) so the contract reads the real tree.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("repo root not found at %s", root)
	}
	return root
}

// allowedLicenseFiles is the EXACT set of license-named files the
// repository may carry (repo-relative, exact spelling). Anything else
// matching the license-pattern below must fail the release.
var allowedLicenseFiles = map[string]bool{
	"LICENSE":             true, // root license summary (the mixed model)
	"LICENSE-APACHE":      true, // Apache-2.0 text (authority)
	"LICENSE-PROPRIETARY": true, // Parsaetak Proprietary License (authority)
	"LICENSE-MAP.md":      true, // component classification authority
	"LICENSE.md":          true, // v1.7.1 human-facing entry point (index only)
	"NOTICE.md":           true, // attribution notices
}

// isLicenseFileName reports whether a name is a license-file candidate
// under ANY common spelling, case-insensitively (LICENSE/LICENSE.md/
// LICENCE/license-APACHE/COPYING-style handles are covered by the
// licence/licence-prefix + known suffixes; unrelated Markdown is not).
// codeExtensions are implementation files ABOUT licensing (a license
// command, a contract test) — documents, not licenses.
var codeExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true,
	".py": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
	".hpp": true, ".cc": true, ".java": true, ".sh": true, ".ps1": true,
}

func isLicenseFileName(name string) bool {
	base := strings.ToLower(name)
	// A source-code file about licensing is not a license document.
	if ext := filepath.Ext(base); codeExtensions[ext] {
		return false
	}
	prefixes := []string{"license", "licence", "copying"}
	for _, p := range prefixes {
		if base == p || strings.HasPrefix(base, p+"-") || strings.HasPrefix(base, p+"_") ||
			strings.HasPrefix(base, p+".") {
			return true
		}
	}
	return false
}

func TestLicenseFileSetIsExact(t *testing.T) {
	root := repoRoot(t)
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip VCS/vendor trees — vendored third-party licenses are their
		// own legal requirement and are covered by NOTICE.md, not this
		// contract.
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "dist" ||
				name == ".npm" || name == "bin" || name == "testresults" {
				return filepath.SkipDir
			}
			return nil
		}

		if isLicenseFileName(d.Name()) {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Every license-named file must be in the allowed set…
	for _, path := range found {
		if !allowedLicenseFiles[path] {
			t.Errorf("redundant license file %q — the v1.7.1 license entry point is LICENSE.md; add new legal documents to LICENSE-MAP.md/NOTICE.md instead, or change this contract deliberately", path)
		}
	}

	// …and every allowed file must actually exist (no silent removal of
	// an authority).
	for path := range allowedLicenseFiles {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("authoritative license file %q missing: %v", path, err)
		}
	}
}

func TestLicenseMDIsTheHumanFacingIndex(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "LICENSE.md"))
	if err != nil {
		t.Fatalf("LICENSE.md must exist (the v1.7.1 human-facing entry point): %v", err)
	}
	content := string(data)

	// The index must point at every authority…
	for _, authority := range []string{
		"LICENSE-APACHE", "LICENSE-PROPRIETARY", "LICENSE-MAP.md", "NOTICE.md", "LICENSE",
	} {
		if !strings.Contains(content, authority) {
			t.Errorf("LICENSE.md does not reference the authority %q", authority)
		}
	}

	// …and must stay an INDEX: it must not grow a second full license
	// text (the "accidentally copy the Apache text in" failure class).
	for _, marker := range []string{
		"Apache License\n   Version 2.0, January 2004",
		"PARSAETAK PROPRIETARY LICENSE\n=============================",
	} {
		if strings.Contains(content, marker) {
			t.Errorf("LICENSE.md must not embed a full license text (marker %q) — link to the authority instead", marker[:40])
		}
	}

	// Trademark + contact coverage (§6 required content).
	for _, want := range []string{"trademark", "Parsaetak"} {
		low := strings.ToLower(content)
		if !strings.Contains(low, strings.ToLower(want)) {
			t.Errorf("LICENSE.md missing required content: %q", want)
		}
	}
}

func TestLicenseSpellingContract(t *testing.T) {
	// The British spelling "licence" must not reintroduce itself: the
	// authoritative set uses "license" spellings only.
	root := repoRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(strings.ToLower(e.Name()), "licence") {
			t.Errorf("redundant licence-spelled file %q — use the LICENSE spelling family", e.Name())
		}
	}
}
