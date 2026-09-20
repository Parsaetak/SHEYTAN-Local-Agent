//go:build !headless

// Package desktop provides the native cross-platform desktop shell.
package desktop

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/platform"
	"github.com/Parsaetak/SHEYTAN-local-agent/web"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	defaultWidth  = 1440
	defaultHeight = 900
	minimumWidth  = 1024
	minimumHeight = 680
)

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

	staticFS, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		fmt.Println("desktop: embedded frontend:", err)
		return 1
	}

	assetHandler := application.AssetFileServerFS(staticFS)
	apiHandler := srv.Handler()

	app := application.New(application.Options{
		Name:        config.AppName,
		Description: "SHEYTAN Local Agent",
		Assets: application.AssetOptions{
			Handler: desktopHandler(assetHandler, apiHandler),
		},
	})

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
