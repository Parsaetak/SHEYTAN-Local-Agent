//go:build !headless

// Package desktop provides the native cross-platform desktop shell.
package desktop

import (
        "fmt"
        "io/fs"
        "net/http"
        "os"
        "strings"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
        "github.com/Parsaetak/SHEYTAN-local-agent/web"
        "github.com/wailsapp/wails/v3/pkg/application"
        "github.com/wailsapp/wails/v3/pkg/events"
)

const (
        defaultWidth  = 1440
        defaultHeight = 900
        minimumWidth  = 1024
        minimumHeight = 680
)

// acceptanceCDPPort reads the TEST-ONLY WebView2 remote-debugging port
// switch (v1.10.0, the Windows live-answer acceptance repair — run
// 38135751846). It returns "" in every normal launch: nothing in the
// product, the installers or the docs ever sets
// SHEYTAN_ACCEPTANCE_CDP_PORT; only the acceptance harness does.
//
// WHY THIS EXISTS (derived from the pinned wails v3.0.0-beta.16 sources,
// the exact failure signature of run 38135751846's "Windows live-answer
// acceptance" step: ~60s harness stall, exit 1, after a green desktop
// runtime smoke): the harness used to pass the CDP switch through the
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS environment variable. Wails'
// default pure-Go WebView2 loader
// (internal/webview2/webviewloader, //go:build windows &&
// !native_webview2loader) explicitly NEUTERS that channel:
// preventEnvAndRegistryOverrides() runs at package init and executes
// os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "") to pin the
// environment against external overrides — so the harness's switch is
// erased inside the app process BEFORE any WebView2 environment is
// created. The debugging port never opens, the harness's bounded CDP
// attach window (60s) times out, and the gate fails honestly. The
// programmatic route — application.Options.Windows.AdditionalBrowserArgs
// — is the one channel Wails composes itself and hands to
// CreateCoreWebView2EnvironmentWithOptions; it survives the env pinning.
//
// TRUST BOUNDARY: this switch arms a WebView2 DevTools endpoint bound to
// the local CDP port. It is inert unless the caller (the acceptance
// harness on a test machine) explicitly exports the variable; the
// production launchers never do. The harness documents the switch as
// TEST-ONLY and never ships it.
func acceptanceCDPPort() string {
        port := strings.TrimSpace(os.Getenv("SHEYTAN_ACCEPTANCE_CDP_PORT"))

        if port == "" {
                return ""
        }

        // Minimal shape check: the value becomes part of a browser command
        // line. Digits only, bounded length — a malformed value must fail
        // the harness's CDP attach (port closed), never corrupt the args.
        if len(port) > 5 {
                return ""
        }

        for _, r := range port {
                if r < '0' || r > '9' {
                        return ""
                }
        }

        return port
}

// Run starts the SHEYTAN native desktop application and blocks until the
// desktop application exits.
//
// The React production assets and the Go API are served through a single
// in-process HTTP handler owned by the Wails asset layer. No external browser,
// localhost listener, or frontend server is required in production.
func Run(cfg *config.Config) int {
        if cfg == nil {
                fmt.Println("desktop: missing configuration")
                return 1
        }

        // v1.2.0: register the process AppUserModelID BEFORE the first window
        // exists, so taskbar grouping, jump lists and notifications identify
        // the app as Parsaetak.SHEYTAN-LA. On platforms without the concept
        // this is an honest no-op.
        _ = platform.SetAppUserModelID(config.AppUserModelID)

        srv, err := api.New(cfg)
        if err != nil {
                fmt.Println("desktop: initialize server:", err)
                return 1
        }
        defer srv.Close()

        if err := srv.EnsureSetup(); err != nil {
                fmt.Println("desktop: setup:", err)
                return 1
        }

        // v1.9.5 (P0 — sequencing + failure semantics): the loopback
        // activity transport starts ONLY after the backend setup has
        // succeeded, and a transport failure is a HARD, VISIBLE fault —
        // the desktop shell exits non-zero instead of silently exposing
        // an app whose core interaction (live streamed answers) is
        // broken. v1.9.4 started the transport before setup and logged
        // the failure as non-fatal, which let a broken transport launch.
        if _, lbErr := srv.StartLoopback(); lbErr != nil {
                fmt.Println("desktop: loopback activity transport unavailable:", lbErr)
                return 1
        }

        staticFS, err := fs.Sub(web.StaticFS, "static")
        if err != nil {
                fmt.Println("desktop: embedded frontend:", err)
                return 1
        }

        assetHandler := application.AssetFileServerFS(staticFS)
        apiHandler := srv.Handler()

        appOpts := application.Options{
                Name:        config.AppName,
                Description: "SHEYTAN Local Agent",
                Assets: application.AssetOptions{
                        Handler: desktopHandler(assetHandler, apiHandler),
                },
        }

        // v1.10.0 TEST-ONLY CDP switch — see acceptanceCDPPort. The
        // programmatic AdditionalBrowserArgs channel is the one Wails
        // composes itself and hands to CreateCoreWebView2EnvironmentWithOptions,
        // so the switch survives the pure-Go loader's env-var pinning that
        // erased the harness's WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS value
        // (the run 38135751846 acceptance failure mechanism).
        if port := acceptanceCDPPort(); port != "" {
                appOpts.Windows = application.WindowsOptions{
                        AdditionalBrowserArgs: []string{
                                "--remote-debugging-port=" + port,
                        },
                }
        }

        app := application.New(appOpts)

        window := app.Window.NewWithOptions(application.WebviewWindowOptions{
                Name:             "main-window",
                Title:            config.AppShortName + " — " + config.AppDescription,
                Width:            defaultWidth,
                Height:           defaultHeight,
                MinWidth:         minimumWidth,
                MinHeight:        minimumHeight,
                BackgroundColour: application.NewRGB(18, 18, 20),
                URL:              "/",
                Hidden:           false,
                DisableResize:    false,
        })

        window.Center()
        window.Show()

        // v1.9.2 (P0 — verified against the wails v3.0.0-beta.16 sources):
        // a webview window's WM_CLOSE (Windows) / GTK close-request (Linux)
        // only EMITS the platform WindowClosing event and destroys the
        // window — the application event loop keeps running with zero
        // windows. On Windows only the hidden "__wails_hidden_mainthread"
        // window's WM_CLOSE/WM_DESTROY triggers globalApplication.Quit()
        // (application_windows.go), and no quit-on-last-window logic exists
        // for webview windows on either platform. Without this binding,
        // closing the main window leaves a zombie process with the backend
        // still running and the deferred srv.Close() never executing —
        // measured as the authoritative Actions 37869632400 Windows smoke
        // failure (process survived CloseMainWindow + 15s).
        //
        // SHEYTAN-LA is a single-window application with no tray:
        // closing the main window MUST terminate the lifecycle so
        // app.Run() returns and the deferred cleanup runs.
        window.OnWindowEvent(events.Common.WindowClosing,
                func(*application.WindowEvent) {
                        app.Quit()
                })

        if err := app.Run(); err != nil {
                fmt.Println("desktop: application:", err)
                return 1
        }

        return 0
}

// desktopHandler routes application API traffic to the Go backend and all
// other traffic to Wails' embedded asset server.
//
// Keeping both paths in one handler makes the production executable fully
// self-contained while preserving the existing REST and WebSocket API
// contract used by the React frontend.
func desktopHandler(assetHandler http.Handler, apiHandler http.Handler) http.Handler {
        mux := http.NewServeMux()

        mux.Handle("/api/", apiHandler)
        mux.Handle("/ws/", apiHandler)
        mux.Handle("/", assetHandler)

        return mux
}
