package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// RunWithDefaultFn dispatches to the right subcommand. If no command is
// given, runs `defaultFn()`. The log catcher is booted first so every
// subcommand is recorded.
func RunWithDefaultFn(defaultFn func() int) int {
	cfg, err := config.Load(configPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		return 1
	}
	_ = cfg.EnsureDirs()

	// Boot the log catcher (app.log, tools.jsonl, llm.jsonl, crashes/).
	mgr, err := logging.New(cfg.LogsDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "log catcher:", err)
	} else {
		logging.SetDefault(mgr)
		logging.SetVersion(config.AppVersion)
		defer mgr.Close()
	}
	// v1.2.2: one unambiguous session separator BEFORE anything else -
	// app.log persists across versions/boots, so historical startup
	// entries (e.g. v0.8.0) previously mixed indistinguishably with the
	// current run. Everything above a banner is verifiably historical.
	logging.Default().SessionBanner(brand.FullName, config.AppVersion)

	logging.Default().Info(
		"boot",
		"%s v%s starting (%s/%s, provider=%s)",
		brand.FullName,
		config.AppVersion,
		runtime.GOOS,
		runtime.GOARCH,
		cfg.ProviderKind(),
	)

	// Crash catcher: any panic in a command becomes a crash-*.log file
	// instead of a silent exit.
	exitCode := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 16384)
				n := runtime.Stack(buf, false)
				path := logging.Default().Crash(r, buf[:n])
				fmt.Fprintf(
					os.Stderr,
					"panic: %v\n(crash report: %s)\n",
					r,
					path,
				)
				exitCode = 1
			}
		}()
		exitCode = dispatch(cfg, defaultFn)
	}()

	logging.Default().Info("boot", "exit code %d", exitCode)
	return exitCode
}

func dispatch(cfg *config.Config, defaultFn func() int) int {
	args := os.Args[1:]

	if len(args) == 0 {
		if defaultFn != nil {
			return defaultFn()
		}
		return Sysinfo(cfg)
	}

	switch args[0] {
	case "ask", "a":
		return Ask(cfg, args[1:])

	case "serve", "s", "web", "ui":
		return Serve(cfg, args[1:])

	case "gui", "desktop":
		if defaultFn != nil {
			return defaultFn()
		}
		return 0

	case "version", "-v", "--version":
		fmt.Printf("%s v%s\n", config.AppName, config.AppVersion)
		fmt.Printf("  %s\n", brand.Notice())
		fmt.Printf("  go: %s\n", goVersion())
		fmt.Printf("  os: %s/%s\n", osName(), osArch())
		return 0

	case "doctor":
		return Doctor(cfg)

	case "install":
		return Install(cfg)

	case "sysinfo":
		return Sysinfo(cfg)

	case "setup":
		return Setup(cfg)

	case "stress":
		return Stress(cfg)

	case "logs":
		return Logs(cfg, args[1:])

	case "update":
		return Update(cfg, args[1:])

	case "diagnostics":
		return Diagnostics(cfg, args[1:])

	case "license":
		return License(cfg)

	case "context", "ai-context":
		return AICtx(cfg, args[1:])

	case "help", "-h", "--help":
		printHelp()
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func configPath() string {
	// Portable mode: config.json lives next to the executable, inside the
	// SHEYTAN-Local-Agent folder (SHEYTAN_DATA_DIR can still override).
	return config.DefaultPath()
}

func printHelp() {
	fmt.Println(
		brand.Trademark + " " + config.AppName + " v" + config.AppVersion,
	)
	fmt.Println(brand.Notice())
	fmt.Println()

	fmt.Println("Usage: sheytan-local-agent <command> [options]")
	fmt.Println()

	fmt.Println("Commands:")
	fmt.Println("  (no args)    Launch the native Windows desktop GUI (default, Windows-only)")
	fmt.Println("  ask          Headless agent turn:  ask \"do anything\"")
	fmt.Println("  serve        Start the HTTP server only (no GUI)")
	fmt.Println("  setup        Run the first-run setup wizard (CLI)")
	fmt.Println("  install      Ensure all components are installed and check for updates")
	fmt.Println("  doctor       Run a full health check")
	fmt.Println("  sysinfo      Print system capabilities + recommended knobs")
	fmt.Println("  logs         Show recent logs + aggregated tool/LLM stats")
	fmt.Println("  update       Check for a llama.cpp engine update (scheduled: daily/weekly/monthly/off)")
	fmt.Println("  diagnostics  Export a diagnostics zip (logs, stats, sysinfo, config)")
	fmt.Println("  license      Print the SHEYTAN™ trademark + license")
	fmt.Println("  context      Show / regenerate the AI instruction file (AI-CONTEXT.md)")
	fmt.Println("  stress       Run the stress test suite")
	fmt.Println("  version      Print version info")
	fmt.Println("  help         Show this help")

	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --port N            Override HTTP port (default 8765)")
	fmt.Println("  --host ADDR         Override bind host (default 127.0.0.1)")
	fmt.Println("  --no-update-check   Skip the per-launch component diff")
	fmt.Println("  --base-url URL      Override llama.cpp base URL")

	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  SHEYTAN_PROVIDER=local|remote        LLM backend selector")
	fmt.Println("  SHEYTAN_REMOTE_BASE_URL=...          OpenAI-compatible endpoint")
	fmt.Println("  SHEYTAN_REMOTE_API_KEY=...           API key for the endpoint")
	fmt.Println("  SHEYTAN_REMOTE_MODEL=...             Model name on the endpoint")
	fmt.Println("  SHEYTAN_BROWSER_PATH=...             Chrome/Edge executable override")
}

func goVersion() string {
	return runtime.Version()
}

func osName() string {
	return runtime.GOOS
}

func osArch() string {
	return runtime.GOARCH
}
