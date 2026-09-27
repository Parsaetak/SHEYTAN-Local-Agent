# LICENSE.md — SHEYTAN-Local-Agent licensing

Copyright © 2024–2026 Parsaetak (https://github.com/Parsaetak). All rights
reserved.

This is the **one human-facing licensing document** for
SHEYTAN-Local-Agent ("the Software"). It consolidates the complete
licensing picture: the mixed-license model, the component classification,
the third-party attribution notices, the trademark notice, and the
governance/contact points. It does not modify, restate or replace the
**authoritative legal texts** — those are the plain documents listed in
[§7](#7-the-authoritative-legal-documents), and they govern; this
document never overrules them.

---

## 1. The licensing model

SHEYTAN-Local-Agent is distributed under a deliberately **conservative
mixed licensing model**. Every file in this repository — and every
artifact built from it — carries exactly one of two licenses:

| License | File | Scope |
|---|---|---|
| Parsaetak Proprietary License v1.1 | [`LICENSE-PROPRIETARY`](LICENSE-PROPRIETARY) | Everything classified **Proprietary** (the default) |
| Apache License 2.0 | [`LICENSE-APACHE`](LICENSE-APACHE) | Components explicitly designated **Open** |

Two rules define the whole model:

1. **The default is Proprietary.** Material becomes open only by explicit
   designation in [§2](#2-component-classification) — never by omission.
2. **The classification decides.** [§2](#2-component-classification) is
   the single authority for which license governs which component. When a
   file's license is unclear, the classification decides it.

The classification attaches to **actual material** (code, documentation,
assets, implementations, and other legally relevant files). It does not
claim that abstract ideas, techniques, or functionality are inherently
protected IP; the proprietary grant covers the concrete expressions
classified below.

---

## 2. Component classification

### Open components (Apache-2.0)

| Component | Path | Notes |
|---|---|---|
| Humanize formatting utilities | `internal/humanize/` | Pure, self-contained formatting helpers (bytes, durations, counts) with no product-specific logic, no state, and no coupling to any SHEYTAN subsystem. Carries `SPDX-License-Identifier: Apache-2.0` file headers. |

These components may be reused under Apache-2.0 **as extracted units**.
They remain part of the repository's tree; the classification is by
component, so every file under the listed path is covered.

### Proprietary components

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
| Governance files | `LICENSE`, `LICENSE-APACHE`, `LICENSE-PROPRIETARY`, `LICENSE.md`, `CONTRIBUTING.md`, `SECURITY.md`, `SIGNATURE` | Legal and governance material. |
| Test suites | all `*_test.go`, `e2e/*.spec.ts`, fixtures except where a component is designated Open | Product verification records exercising proprietary components. |

### How to determine the license of a file

1. Find the component the file belongs to in the tables above; the
   classification is authoritative.
2. Files inside an Apache-2.0-designated component carry an
   `SPDX-License-Identifier: Apache-2.0` header.
3. Everything not explicitly classified as open above is Proprietary
   under `LICENSE-PROPRIETARY` — the conservative default: material is
   open ONLY by explicit designation, never by omission.

---

## 3. Third-party software and attribution notices

SHEYTAN-Local-Agent builds on, embeds, or downloads third-party software
(Go modules, frontend npm packages, provisioning artifacts and the
native-engine test tooling). Each component remains under its own
license; nothing in this repository modifies any third-party licensing
term. This section carries the attribution notices required when
redistributing builds (including the Apache-2.0 §4(d) notices).

### llama.cpp — the inference engine

The managed llama.cpp engine binaries are downloaded at runtime from
https://github.com/ggml-org/llama.cpp (MIT license) into the
application's managed bin directory and executed as a subprocess.

    Copyright (c) 2023-2026 The ggml authors
    MIT License — https://github.com/ggml-org/llama.cpp/blob/master/LICENSE

llama.cpp is NOT distributed inside this repository or its release
archives.

### Third-party material overview

| Component | License | Where it lives |
|---|---|---|
| llama.cpp (downloaded engine) | MIT | fetched at runtime into the managed bin directory — never committed |
| Wails v3 | MIT | build-time Go dependency |
| React, Zustand, react-markdown, remark/rehype stack | MIT | build-time frontend dependencies |
| gorilla/websocket | BSD-style | build-time Go dependency |
| chromedp + cdproto | MIT | build-time Go dependency |
| bild, xdg, winres, go-isatty, nfnt/resize, gobwas/* | MIT / BSD-style / ISC-style (per package) | build-time Go dependencies |
| Go standard library + golang.org/x/* | BSD-style (Go license) | toolchain and build-time dependencies |
| Highlight.js (via rehype-highlight) | BSD-3-Clause | frontend dependency |

### Go dependencies (linked into the binary)

| Package | License | Copyright |
|---|---|---|
| github.com/wailsapp/wails/v3 | MIT | Copyright (c) 2018-Present Lea Anthony |
| github.com/gorilla/websocket | BSD-style | Copyright (c) 2013 The Gorilla WebSocket Authors |
| github.com/chromedp/chromedp, github.com/chromedp/cdproto | MIT | Copyright (c) 2016-2025 Kenneth Shaw |
| github.com/anthonynsimon/bild | MIT | Copyright (c) 2016-2024 Anthony Simon |
| github.com/adrg/xdg | MIT | Copyright (c) 2014 Adrian-George Bostan |
| github.com/tc-hib/winres | ISC-style | Copyright (c) 2021 Thomas Combeléran |
| github.com/mattn/go-isatty | MIT | Copyright (c) Yasuhiro Matsumoto |
| github.com/nfnt/resize | BSD-style | Copyright (c) 2012 Jan Schlicht |
| github.com/gobwas/* | MIT | Copyright (c) 2017 Sergey Kamardin et al. |
| github.com/go-json-experiment/json | BSD-style (Go) | Copyright (c) 2020 The Go Authors |
| golang.org/x/net, golang.org/x/sys, golang.org/x/image | BSD-style (Go) | Copyright The Go Authors |

### Frontend dependencies (bundled into web/static by the build)

| Package | License | Copyright |
|---|---|---|
| react, react-dom | MIT | Copyright (c) Meta Platforms, Inc. and affiliates |
| zustand | MIT | Copyright (c) 2019 Paul Henschel |
| react-markdown | MIT | Copyright (c) Espen Hovlandsdal et al. |
| remark-gfm | MIT | Copyright (c) Titus Wormer et al. |
| rehype-highlight (Highlight.js) | BSD-3-Clause | Copyright (c) Ivan Sagalaev and other contributors |

The exact license texts ship with each package in its published archive
and are available in the respective repositories/registries.

---

## 4. Trademarks

SHEYTAN™ and the SHEYTAN logo are trademarks of Parsaetak. They identify
the Software and its distribution; no license in this repository grants
any right to use the SHEYTAN name, logo or branding beyond what trademark
law allows, and nothing here implies endorsement of derived works. The
trademark grant and restrictions are stated in `LICENSE-PROPRIETARY` §3.

---

## 5. Governance — changing a classification

Only the repository maintainer (Parsaetak) may reclassify a component:

1. Move the component between the tables in [§2](#2-component-classification)
   with the rationale.
2. For a move to Open: add `SPDX-License-Identifier: Apache-2.0` headers
   to every file in the component and confirm the component has no
   coupling to proprietary mechanisms (or the coupling is one-directional:
   proprietary → open).
3. For a move to Proprietary: remove the Apache headers, and confirm no
   downstream Apache-licensed extraction depends on it.
4. Describe the change in the commit.

---

## 6. Contact

Licensing questions, proprietary-use inquiries and exceptions:
**https://github.com/Parsaetak** (open an issue or use the contact
channel listed there).

---

## 7. The authoritative legal documents

- [`LICENSE`](LICENSE) — the root license summary: the mixed-model text
  itself, introduced with v1.6.1 (regenerated from `internal/brand`).
- [`LICENSE-APACHE`](LICENSE-APACHE) — the Apache License 2.0 text,
  governing the explicitly designated open components.
- [`LICENSE-PROPRIETARY`](LICENSE-PROPRIETARY) — the Parsaetak
  Proprietary License v1.1, governing everything else.

These three plain documents are the legally binding authorities. This
Markdown document is the human-facing consolidation; where any wording
appears to differ, the authoritative texts govern.
