# LICENSE.md — SHEYTAN-Local-Agent licensing at a glance

Copyright © 2024–2026 Parsaetak (https://github.com/Parsaetak). All rights
reserved.

This file is the HUMAN-FACING ENTRY POINT to the licensing of
SHEYTAN-Local-Agent ("the Software"). It explains the model and where the
authoritative texts live. It does not modify, restate or replace them —
the authoritative documents listed below govern, and this index never
overrules them.

## The licensing model

SHEYTAN-Local-Agent is distributed under a deliberately **conservative
mixed licensing model**. Every file in this repository — and every
artifact built from it — carries exactly one of two licenses:

| License | File | Scope |
|---|---|---|
| Parsaetak Proprietary License v1.1 | [`LICENSE-PROPRIETARY`](LICENSE-PROPRIETARY) | Everything classified **Proprietary** (the default) |
| Apache License 2.0 | [`LICENSE-APACHE`](LICENSE-APACHE) | Components explicitly designated **Open** |

Two rules define the whole model:

1. **The default is Proprietary.** Material becomes open only by explicit
   designation in [`LICENSE-MAP.md`](LICENSE-MAP.md) — never by omission.
2. **The map decides.** [`LICENSE-MAP.md`](LICENSE-MAP.md) is the single
   authority for which license governs which component. When a file's
   license is unclear, the map decides it.

Currently the open set is deliberately tiny (self-contained utility
packages carrying `SPDX-License-Identifier: Apache-2.0` headers, such as
`internal/humanize/`). See the map's "Open components" table for the
authoritative list.

## Authoritative documents

- [`LICENSE`](LICENSE) — the root license summary: the mixed-model text
  itself, introduced with v1.6.1. Start here for the binding summary.
- [`LICENSE-MAP.md`](LICENSE-MAP.md) — the component classification
  authority (which license governs which path).
- [`LICENSE-APACHE`](LICENSE-APACHE) — the Apache License 2.0 text,
  governing the explicitly designated open components.
- [`LICENSE-PROPRIETARY`](LICENSE-PROPRIETARY) — the Parsaetak
  Proprietary License v1.1, governing everything else.
- [`NOTICE.md`](NOTICE.md) — the attribution notices required when
  redistributing builds (including the Apache-2.0 §4(d) notices and
  third-party attributions).

## Third-party software

SHEYTAN-Local-Agent builds on third-party software (Go modules, frontend
npm packages, provisioning artifacts and the native-engine test tooling).
Their licenses and notices are carried in [`NOTICE.md`](NOTICE.md); the
underlying distributions carry their own license texts. Nothing in this
repository modifies any third-party licensing term.

## Trademarks

SHEYTAN™ and the SHEYTAN logo are trademarks of Parsaetak. They identify
the Software and its distribution; no license in this repository grants
any right to use the SHEYTAN name, logo or branding beyond what trademark
law allows, and nothing here implies endorsement of derived works.

## Contact

Licensing questions, proprietary-use inquiries and exceptions:
**https://github.com/Parsaetak** (open an issue or use the contact
channel listed there).
