// license_contract_test.go — v1.7.2: the deterministic license-file
// hygiene contract (REWRITTEN for the consolidated single-document
// layout).
//
// Section 6 of the v1.7.1 task created LICENSE.md as an INDEX pointing at
// LICENSE-MAP.md and NOTICE.md. The v1.7.2 task completes the literal
// requirement: exactly ONE human-facing Markdown licensing document —
// LICENSE.md — carrying the consolidated classification AND third-party
// attribution, with LICENSE-MAP.md and NOTICE.md REMOVED and the
// authoritative legal texts (LICENSE, LICENSE-APACHE,
// LICENSE-PROPRIETARY — all non-Markdown) preserved.
//
// This test prevents regression in BOTH directions:
//
//   - the authoritative non-Markdown legal file set is EXACT (no silent
//     removal of a legal authority, no silent addition);
//   - LICENSE.md exists and carries the consolidated CONTENT (not a bare
//     index: classification + third-party attribution + trademark +
//     governance must be present in the document itself);
//   - NO second human-facing license/notice Markdown may appear anywhere
//     in the repository, under ANY spelling: LICENSE-MAP.md, NOTICE.md,
//     Licence.md, LICENCE.md, license.md, notice.md, COPYING.md, …
//   - legitimate NON-Markdown legal authorities (LICENSE, LICENSE-APACHE,
//     LICENSE-PROPRIETARY) are never falsely classified as duplicates;
//     source-code files ABOUT licensing (a license command, this test)
//     are documents about the topic, not license documents.
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

// authoritativeLegalFiles is the EXACT set of NON-Markdown legal
// authority files the repository must carry (repo-relative, exact
// spelling). These are legally binding texts, not human-facing
// duplicates — the consolidation must never remove them.
var authoritativeLegalFiles = map[string]bool{
        "LICENSE":             true, // root license summary (regenerated from internal/brand)
        "LICENSE-APACHE":      true, // Apache-2.0 text (authority)
        "LICENSE-PROPRIETARY": true, // Parsaetak Proprietary License (authority)
}

// humanFacingLicenseMarkdown is the EXACT set of human-facing license
// Markdown the repository may carry: ONE document.
var humanFacingLicenseMarkdown = map[string]bool{
        "LICENSE.md": true, // the consolidated v1.7.2 licensing document
}

// codeExtensions are implementation files ABOUT licensing (a license
// command, a contract test) — documents, not licenses.
var codeExtensions = map[string]bool{
        ".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true,
        ".py": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
        ".hpp": true, ".cc": true, ".java": true, ".sh": true, ".ps1": true,
}

// isLicenseFileName reports whether a name is a license/notice-file
// candidate under ANY common spelling, case-insensitively
// (LICENSE/LICENCE/license/notice/COPYING prefixes with -, _ or .
// continuations). Non-Markdown authorities and code files about
// licensing are distinguished by the CALLER, not here.
func isLicenseFileName(name string) bool {
        base := strings.ToLower(name)
        if ext := filepath.Ext(base); codeExtensions[ext] {
                return false // a source-code file about licensing is not a license
        }
        prefixes := []string{"license", "licence", "notice", "copying"}
        for _, p := range prefixes {
                if base == p || strings.HasPrefix(base, p+"-") || strings.HasPrefix(base, p+"_") ||
                        strings.HasPrefix(base, p+".") {
                        return true
                }
        }
        return false
}

// walkLicenseFiles collects every license-named file in the repository
// (skipping VCS/vendor/build trees), case-insensitively.
func walkLicenseFiles(t *testing.T) []string {
        t.Helper()
        root := repoRoot(t)
        var found []string
        err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
                if err != nil {
                        return err
                }
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
        return found
}

// TestExactlyOneHumanFacingLicenseMarkdown: LICENSE.md is the ONLY
// human-facing license Markdown — the consolidated v1.7.2 layout. Any
// reintroduced duplicate (LICENSE-MAP.md, NOTICE.md, Licence.md,
// LICENCE.md, license.md, notice.md, …) fails the release.
func TestExactlyOneHumanFacingLicenseMarkdown(t *testing.T) {
        found := walkLicenseFiles(t)

        var mdFiles []string
        for _, rel := range found {
                if strings.EqualFold(filepath.Ext(rel), ".md") {
                        mdFiles = append(mdFiles, rel)
                        if !humanFacingLicenseMarkdown[rel] {
                                t.Errorf("redundant human-facing license Markdown %q — LICENSE.md is the ONE consolidated licensing document; merge the information there and delete the duplicate", rel)
                        }
                }
        }

        if len(mdFiles) == 0 {
                t.Fatal("LICENSE.md is missing — the one human-facing licensing document must exist")
        }

        // The consolidated document must actually EXIST.
        if _, err := os.Stat(filepath.Join(repoRoot(t), "LICENSE.md")); err != nil {
                t.Fatalf("LICENSE.md must exist: %v", err)
        }
}

// TestAuthoritativeLegalFilesAreExact: the non-Markdown legal authority
// set is EXACT — no silent removal (destruction of required legal text)
// and no unvetted addition.
func TestAuthoritativeLegalFilesAreExact(t *testing.T) {
        found := walkLicenseFiles(t)

        seen := map[string]bool{}
        for _, rel := range found {
                if !strings.EqualFold(filepath.Ext(rel), ".md") {
                        seen[rel] = true
                        if !authoritativeLegalFiles[rel] {
                                t.Errorf("unexpected non-Markdown license file %q — add it to the authoritative set deliberately (and extend LICENSE.md), or remove it", rel)
                        }
                }
        }

        for path := range authoritativeLegalFiles {
                if !seen[path] {
                        t.Errorf("authoritative legal file %q missing — the consolidation must PRESERVE the legally binding texts", path)
                }
        }
}

// TestLicenseMDContainsConsolidatedContent: LICENSE.md carries the actual
// consolidated INFORMATION (v1.7.2 completes the literal requirement —
// it must NOT be a bare index that says "see other files"):
//
//   - the mixed-model classification (open + proprietary components);
//   - the third-party attribution (llama.cpp + dependency tables);
//   - the trademark notice;
//   - governance/contact;
//   - the relationship to the authoritative legal texts.
func TestLicenseMDContainsConsolidatedContent(t *testing.T) {
        root := repoRoot(t)
        data, err := os.ReadFile(filepath.Join(root, "LICENSE.md"))
        if err != nil {
                t.Fatalf("LICENSE.md must exist: %v", err)
        }
        content := string(data)
        low := strings.ToLower(content)

        // (a) The consolidated classification is present in the document.
        for _, want := range []string{
                "Open components",          // the classification section
                "Proprietary components",   // the classification section
                "internal/humanize/",       // the designated open component
                "Apache-2.0",               // the open license routing
                "LICENSE-PROPRIETARY",      // the proprietary license routing
                "SPDX-License-Identifier",  // the how-to-determine procedure
        } {
                if !strings.Contains(content, want) && !strings.Contains(low, strings.ToLower(want)) {
                        t.Errorf("LICENSE.md missing consolidated classification content: %q", want)
                }
        }

        // (b) The third-party attribution (former NOTICE.md content) is
        // present in the document.
        for _, want := range []string{
                "llama.cpp",                          // the engine
                "The ggml authors",                    // the llama.cpp copyright notice
                "gorilla/websocket",                   // a Go dependency
                "react",                               // a frontend dependency
                "Third-party software",                // the attribution section
        } {
                if !strings.Contains(low, strings.ToLower(want)) {
                        t.Errorf("LICENSE.md missing third-party attribution content (former NOTICE.md): %q", want)
                }
        }

        // (c) Trademark + governance/contact + authority relationship.
        for _, want := range []string{
                "trademark",            // the trademark notice
                "parsaetak",            // the licensor
                "github.com/parsaetak", // the contact point
                "authoritative",        // the relationship to the legal texts
        } {
                if !strings.Contains(low, want) {
                        t.Errorf("LICENSE.md missing required content: %q", want)
                }
        }

        // (d) It must NOT be a bare index: the "see other files" failure
        // class is a document whose classification section only links out.
        // The former LICENSE-MAP.md/NOTICE.md are GONE — referencing them as
        // living authorities proves the document was not consolidated.
        for _, stale := range []string{"LICENSE-MAP.md", "NOTICE.md"} {
                if strings.Contains(content, "]("+stale+")") || strings.Contains(content, "`"+stale+"`") {
                        t.Errorf("LICENSE.md still references %q as a living authority — the v1.7.2 consolidation merged it INTO this document", stale)
                }
        }
}

// TestLicenseSpellingContract: the British spelling "licence" must not
// reintroduce itself anywhere in the tree, and the root carries only the
// sanctioned license files.
func TestLicenseSpellingContract(t *testing.T) {
        root := repoRoot(t)
        entries, err := os.ReadDir(root)
        if err != nil {
                t.Fatal(err)
        }
        sanctioned := map[string]bool{
                "LICENSE": true, "LICENSE-APACHE": true, "LICENSE-PROPRIETARY": true,
                "LICENSE.md": true,
                "NOTICE.md": false, "LICENSE-MAP.md": false, // the merged-away pair
        }
        for _, e := range entries {
                low := strings.ToLower(e.Name())
                if strings.HasPrefix(low, "licence") {
                        t.Errorf("redundant licence-spelled file %q — use the LICENSE spelling family", e.Name())
                }
                if expect, known := sanctioned[e.Name()]; known && !expect {
                        t.Errorf("file %q was merged into LICENSE.md in v1.7.2 and must not reappear", e.Name())
                }
        }
}

// TestLicenseCLIRemainsValid: the license CLI path renders the brand
// constants (the v1.7.2 consolidation must not break cmd/license.go or
// the generated LICENSE).
func TestLicenseCLIRemainsValid(t *testing.T) {
        root := repoRoot(t)

        // The generated LICENSE matches brand.LicenseText (gen-license.go).
        license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
        if err != nil {
                t.Fatalf("LICENSE: %v", err)
        }
        if !strings.Contains(string(license), "CONSERVATIVE MIXED MODEL") {
                t.Error("LICENSE no longer carries the mixed-model summary text (regenerate with scripts/gen-license.go)")
        }
        if strings.Contains(string(license), "LICENSE-MAP.md") || strings.Contains(string(license), "NOTICE.md") {
                t.Error("LICENSE still routes to the merged-away files — regenerate after updating internal/brand")
        }

        // The brand constants reference the consolidated document.
        brandSrc, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
        if err != nil {
                t.Fatalf("brand.go: %v", err)
        }
        if strings.Contains(string(brandSrc), "LICENSE-MAP.md") || strings.Contains(string(brandSrc), "NOTICE.md") {
                t.Error("internal/brand still references the merged-away files")
        }
}
