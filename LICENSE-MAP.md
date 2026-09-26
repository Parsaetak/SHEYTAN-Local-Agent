# LICENSE-MAP.md — the component classification authority

**Version:** 1.1 (v1.6.2 audit: no classification changed — all v1.6.2
work landed in already-classified proprietary components; third-party
exclusions re-verified)
**Maintainer:** Parsaetak — https://github.com/Parsaetak

This file is the single authority for which license governs which
component of SHEYTAN-Local-Agent. When a file's license is unclear, this
map decides it. The model is deliberately **conservative mixed**:

- **Open** components are explicitly designated below and governed by
  [`LICENSE-APACHE`](LICENSE-APACHE) (Apache License 2.0).
- **Proprietary** components are governed by
  [`LICENSE-PROPRIETARY`](LICENSE-PROPRIETARY) (Parsaetak Proprietary
  License v1.1).
- The **default** classification for anything not listed is
  **Proprietary** — material becomes open only by explicit designation
  here, never by omission.

The classification attaches to **actual material** (code, documentation,
assets, implementations, and other legally relevant files). It does not
claim that abstract ideas, techniques, or functionality are inherently
protected IP; the proprietary grant covers the concrete expressions
listed below.

---

## Open components (Apache-2.0)

| Component | Path | Notes |
|---|---|---|
| Humanize formatting utilities | `internal/humanize/` | Pure, self-contained formatting helpers (bytes, durations, counts) with no product-specific logic, no state, and no coupling to any SHEYTAN subsystem. Carries `SPDX-License-Identifier: Apache-2.0` file headers. |

These components may be reused under Apache-2.0 **as extracted units**.
They remain part of the repository's tree; the classification is by
component, so every file under the listed path is covered.

## Proprietary components

Everything else in the repository is Proprietary under
`LICENSE-PROPRIETARY`, including (non-exhaustive, grouped by area):

| Area | Paths | Classification rationale |
|---|---|---|
| SHEYTAN product implementation | `internal/` (all packages except the Open components above), `cmd/`, `main*.go`, `web/` | The product-specific implementation: engine lifecycle, sampling gate, model-selection state machine, accelerator resolver, transactional updater/installer, agent orchestrator, research engine, coding lab, sessions, vision, calibration. |
| Native C++ engine | `native/engine/` | SHEYTAN-specific proprietary inference engine implementation, tokenizer, scheduler, GGUF loader. |
| Frontend (UI/UX) | `src/`, `index.html`, `vite.config.ts`, `tsconfig*.json`, styles | Product-specific implementation and interface designs. |
| Brand, assets and designs | `internal/brand/` (logo SVG and identity constants), `build/` (icon, packaging art), `scripts/gen-syso/logo-512.png` | Trademarked assets and designs. |
| Product documentation | `README.md`, `ARCHITECTURE.md`, `UPDATE.md`, `agent.md`, `ROADMAP.md`, `REPORT.md`, `internal/aicontext/AI-CONTEXT.md`, `worklog.md`, `native/engine/README.md` | Product documentation and engineering records. |
| Build, release and CI machinery | `scripts/`, `packaging/`, `.github/`, `e2e/` (harness), `Makefile`s | Product-specific delivery pipeline. |
| Governance files | `LICENSE`, `LICENSE-APACHE`, `LICENSE-PROPRIETARY`, `LICENSE-MAP.md`, `NOTICE.md`, `CONTRIBUTING.md`, `SECURITY.md`, `SIGNATURE` | Legal and governance material. |
| Test suites | all `*_test.go`, `e2e/*.spec.ts`, fixtures except where a component is designated Open | Product verification records exercising proprietary components. |

## Third-party material (not SHEYTAN's to license)

| Component | License | Where it lives |
|---|---|---|
| llama.cpp (downloaded engine) | MIT | fetched at runtime into the managed bin directory — never committed |
| Wails v3 | MIT | build-time Go dependency |
| React, Zustand, react-markdown, remark/hrehype stack | MIT | build-time frontend dependencies |
| gorilla/websocket | BSD-style | build-time Go dependency |
| chromedp + cdproto | MIT | build-time Go dependency |
| bild, xdg, winres, go-isatty, nfnt/resize, gobwas/* | MIT / BSD-style / ISC-style (per package) | build-time Go dependencies |
| Go standard library + golang.org/x/* | BSD-style (Go license) | toolchain and build-time dependencies |
| Highlight.js (via rehype-highlight) | BSD-3-Clause | frontend dependency |

See [`NOTICE.md`](NOTICE.md) for the required attribution notices when
 redistributing builds that bundle or link these components.

---

## Changing a classification

Only the repository maintainer (Parsaetak) may reclassify a component:

1. Move the component between the tables above with the rationale.
2. For a move to Open: add `SPDX-License-Identifier: Apache-2.0` headers
   to every file in the component and confirm the component has no
   coupling to proprietary mechanisms (or the coupling is one-directional:
   proprietary → open).
3. For a move to Proprietary: remove the Apache headers, and confirm no
   downstream Apache-licensed extraction depends on it.
4. Update this file's version line and describe the change in the commit.
