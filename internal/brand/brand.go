// Package brand holds the SHEYTAN™ identity: trademark attribution,
// copyright, and license text. Every surface (GUI, CLI, docs, diagnostics)
// renders these constants so legal notices stay consistent across releases.
//
// Legal structure:
//   - Copyright holder & licensor:  Parsaetak (https://github.com/Parsaetak)
//   - Trademark:                    SHEYTAN™ — a trademark of Parsaetak,
//     used as the product name for this app.
package brand

import "time"

const (
        // Name is the short product name.
        Name = "SHEYTAN"
        // FullName is the complete product name.
        FullName = "SHEYTAN-Local-Agent"
        // Trademark is the brand with the ™ mark, for display surfaces.
        Trademark = "SHEYTAN™"
        // Licensor is the legal entity that owns the copyright and license.
        Licensor = "Parsaetak"
        // LicensorURL is the official licensor contact point.
        LicensorURL = "https://github.com/Parsaetak"
        // TrademarkNotice is the formal trademark attribution line.
        TrademarkNotice = "SHEYTAN and the SHEYTAN logo are trademarks of Parsaetak."
        // LicenseName is the human name of the license. v1.6.1: the
        // conservative mixed model — Apache-2.0 for explicitly designated
        // open components (LICENSE-APACHE), the Parsaetak Proprietary
        // License v1.1 for the SHEYTAN-specific proprietary material
        // (LICENSE-PROPRIETARY), classified file-by-file in LICENSE-MAP.md.
        LicenseName = "Mixed: Apache-2.0 (designated components) + Parsaetak Proprietary v1.1"

        // SignedBy is the application author/signer — v1.0.8. Every release
        // of SHEYTAN-Local-Agent is authored and signed under this name; it
        // is embedded in the exe version resource (CompanyName), printed in
        // the About dialog, and carried in the SIGNATURE block of each
        // distribution archive.
        SignedBy = "Parsa Tak"
        // SignedByRole is the role line printed under the signature.
        SignedByRole = "Author & Application Signer"
)

// LogoSVG is the full-color flame mark (gradient flame on a deep ember
// disc) used everywhere the brand appears: the app UI, the window icon,
// the splash — and, since v1.0.5, the rendered .exe icon. It lives here so
// the GUI theme and the build-time icon generator can never drift apart.
const LogoSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 24 24">
<defs>
  <linearGradient id="flame" x1="0" y1="1" x2="0" y2="0">
    <stop offset="0%" stop-color="#C9182B"/>
    <stop offset="45%" stop-color="#FF3B30"/>
    <stop offset="80%" stop-color="#FF6B1A"/>
    <stop offset="100%" stop-color="#FFC53D"/>
  </linearGradient>
  <linearGradient id="flameInner" x1="0" y1="1" x2="0" y2="0">
    <stop offset="0%" stop-color="#FFDD55"/>
    <stop offset="100%" stop-color="#FF8A50"/>
  </linearGradient>
</defs>
<circle cx="12" cy="12" r="11.4" fill="#120808"/>
<circle cx="12" cy="12" r="11.4" fill="none" stroke="#FF3B30" stroke-width=".5" opacity=".8"/>
<path d="M13.5 2.2s.74 2.65.74 4.8c0 2.06-1.35 3.73-3.41 3.73-2.07 0-3.63-1.67-3.63-3.73l.03-.36C5.2 6.6 4 9.7 4 13.1c0 4.42 3.58 8 8 8s8-3.58 8-8c0-5.4-2.59-10.2-6.5-13.33z" fill="url(#flame)"/>
<path d="M12.2 10.4c1.5 1.1 2.6 2.4 2.6 4.2 0 1.9-1.3 3.4-3 3.4-1.1 0-2-.6-2.5-1.5-.2 1.5.8 3.3 2.1 4-.5.1-1 .2-1.6.1-2.4-.3-4.1-2.5-3.8-5 .2-1.6 1.2-2.6 2.2-3.6.9-.9 1.7-1.9 2-3.1.6.7 1.3 1.2 2 1.5z" fill="url(#flameInner)" opacity=".9"/>
</svg>`

// CopyrightYears returns the copyright range "2024–<current year>".
func CopyrightYears() string {
        y := time.Now().Year()
        if y <= 2024 {
                return "2024"
        }
        return "2024–" + itoa(y)
}

// Copyright returns the canonical copyright line, e.g.
// "© 2024–2026 Parsaetak. All rights reserved."
func Copyright() string {
        return "© " + CopyrightYears() + " " + Licensor + ". All rights reserved."
}

// Notice returns the short one-line legal footer used in the UI.
func Notice() string {
        return Trademark + " · " + Copyright()
}

// SignatureLine returns the one-line authorship attribution, e.g.
// "Signed by Parsa Tak — Author & Application Signer".
func SignatureLine() string {
        return "Signed by " + SignedBy + " — " + SignedByRole
}

// SignatureBlock returns the full signature text carried in the About
// dialog and the SIGNATURE file of every distribution archive.
func SignatureBlock(version string) string {
        return "SHEYTAN-Local-Agent v" + version + "\n" +
                "Signed by: " + SignedBy + "\n" +
                "Role:      " + SignedByRole + "\n" +
                "Product:   " + FullName + " (" + Trademark + ")\n" +
                "Licensor:  " + Licensor + " <" + LicensorURL + ">\n" +
                Copyright() + "\n" +
                TrademarkNotice + "\n"
}

// LicenseFooter is the compact attribution block shown in About dialogs.
const LicenseFooter = "SHEYTAN™ is a trademark of Parsaetak.\nLicensed under the conservative mixed model — Apache-2.0 for designated open components, Parsaetak Proprietary for SHEYTAN-specific material (see LICENSE-MAP.md).\n" + LicensorURL

func itoa(n int) string {
        if n == 0 {
                return "0"
        }
        var b []byte
        for n > 0 {
                b = append([]byte{byte('0' + n%10)}, b...)
                n /= 10
        }
        return string(b)
}

// LicenseText is the license summary shipped with the app (v1.6.1: the
// conservative mixed-model routing text — the full component
// classification lives in LICENSE-MAP.md; the two licenses live in
// LICENSE-APACHE and LICENSE-PROPRIETARY).
const LicenseText = `
SHEYTAN™ LICENSE — CONSERVATIVE MIXED MODEL
===========================================
Version 1.0 · Introduced with SHEYTAN-Local-Agent v1.6.1

Copyright © 2024–2026 Parsaetak (https://github.com/Parsaetak). All rights
reserved.

SHEYTAN-Local-Agent ("the Software") is distributed under a deliberately
CONSERVATIVE mixed licensing model: every file in this repository and
every artifact built from it carries exactly one of the two licenses
below, recorded in LICENSE-MAP.md (the classification authority).

  1. LICENSE-APACHE      — Apache License 2.0, for the explicitly
                           designated OPEN components listed in
                           LICENSE-MAP.md §Open components.

  2. LICENSE-PROPRIETARY — the Parsaetak Proprietary License v1.1, for
                           the SHEYTAN-specific proprietary mechanisms,
                           the product-specific implementation, the
                           assets and designs, and every other component
                           explicitly classified as Proprietary in
                           LICENSE-MAP.md.

HOW TO DETERMINE THE LICENSE OF A FILE
--------------------------------------
  1. Open LICENSE-MAP.md and find the component the file belongs to.
     The map's classification is authoritative.
  2. Files inside an Apache-2.0-designated component carry an
     "SPDX-License-Identifier: Apache-2.0" header.
  3. Everything not explicitly classified as open in LICENSE-MAP.md is
     Proprietary under LICENSE-PROPRIETARY — the conservative default:
     material is open ONLY by explicit designation, never by omission.

This mixed model classifies ACTUAL material — code, documentation,
assets, implementations, and other legally relevant files. It does not
assert that abstract ideas, techniques, or functionality are inherently
protected intellectual property; protection attaches only to the
concrete expressions classified in the map.

TRADEMARK
---------
"SHEYTAN", "SHEYTAN-Local-Agent", and the SHEYTAN logo are trademarks of
Parsaetak. The trademark grant and restrictions are stated in
LICENSE-PROPRIETARY §3.

THIRD-PARTY MATERIAL
--------------------
The Software bundles, downloads, or links third-party components (the
llama.cpp inference engine, the Wails desktop framework, React and the
frontend toolchain, and others). Those components remain under their own
licenses, which are acknowledged in NOTICE.md. Nothing in this file
changes the license of third-party material.

CONTACT
-------
Licensing and trademark inquiries:
  Parsaetak — https://github.com/Parsaetak
`
