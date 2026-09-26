# NOTICE.md

This file carries the attribution notices required when redistributing
builds of SHEYTAN-Local-Agent, as described in LICENSE-APACHE §4(d) and
good practice for the other licenses in play.

## SHEYTAN-Local-Agent

Copyright © 2024–2026 Parsaetak. All rights reserved.

SHEYTAN and the SHEYTAN logo are trademarks of Parsaetak.

SHEYTAN-Local-Agent is distributed under a conservative mixed licensing
model — see LICENSE (root) and LICENSE-MAP.md for the authoritative
component classification.

## Third-party components bundled, linked or downloaded

SHEYTAN-Local-Agent is built on, embeds, or downloads the following
third-party components. Each remains under its own license; the notices
below are reproduced or summarized as required.

### llama.cpp — the inference engine

The managed llama.cpp engine binaries are downloaded at runtime from
https://github.com/ggml-org/llama.cpp (MIT license) into the
application's managed bin directory and executed as a subprocess.

    Copyright (c) 2023-2026 The ggml authors
    MIT License — https://github.com/ggml-org/llama.cpp/blob/master/LICENSE

llama.cpp is NOT distributed inside this repository or its release
archives.

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
