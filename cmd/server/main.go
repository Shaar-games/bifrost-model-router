package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/maximhq/bifrost/transports/bifrost-http/handlers"
	"github.com/maximhq/bifrost/transports/bifrost-http/lib"
	bifrostserver "github.com/maximhq/bifrost/transports/bifrost-http/server"

	"github.com/applyinnovations/bifrost-model-router/internal/envfile"
	"github.com/applyinnovations/bifrost-model-router/internal/gateway"
	"github.com/applyinnovations/bifrost-model-router/internal/platformpaths"
	"github.com/applyinnovations/bifrost-model-router/internal/routerplugin"
	"github.com/applyinnovations/bifrost-model-router/internal/runtimeconfig"
)

//go:embed all:ui
var uiContent embed.FS

var version = "dev"

func main() {
	opts, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bifrost-model-router:", err)
		os.Exit(1)
	}
	if os.Getenv(supervisedEnv) == "1" {
		if err := run(opts); err != nil {
			fmt.Fprintln(os.Stderr, "bifrost-model-router:", err)
			os.Exit(1)
		}
		return
	}
	if err := supervise(opts.configPath, opts.envPath); err != nil {
		fmt.Fprintln(os.Stderr, "bifrost-model-router:", err)
		os.Exit(1)
	}
}

type serverOptions struct {
	configPath string
	envPath    string
	appDir     string
	addr       string
	coreHost   string
	corePort   string
	chatGPTURL string
	logLevel   string
}

func parseFlags() (serverOptions, error) {
	defaults, err := defaultOptions()
	if err != nil {
		return serverOptions{}, err
	}
	configPath := flag.String("config", defaults.configPath, "declarative Bifrost configuration")
	envPath := flag.String("env-file", defaults.envPath, "provider credential environment file")
	appDir := flag.String("app-dir", defaults.appDir, "writable Bifrost application directory")
	addr := flag.String("addr", "127.0.0.1:8080", "public Codex gateway address")
	coreHost := flag.String("core-host", "127.0.0.1", "internal Bifrost host")
	corePort := flag.String("core-port", "8081", "internal Bifrost port")
	chatGPTURL := flag.String("chatgpt-upstream", "https://chatgpt.com", "ChatGPT upstream origin")
	logLevel := flag.String("log-level", "info", "debug, info, warn, or error")
	flag.Parse()
	return serverOptions{
		configPath: *configPath,
		envPath:    *envPath,
		appDir:     *appDir,
		addr:       *addr,
		coreHost:   *coreHost,
		corePort:   *corePort,
		chatGPTURL: *chatGPTURL,
		logLevel:   *logLevel,
	}, nil
}

func run(opts serverOptions) error {
	envPath := opts.envPath
	configPath := opts.configPath
	appDir := opts.appDir
	addr := opts.addr
	coreHost := opts.coreHost
	corePort := opts.corePort
	chatGPTURL := opts.chatGPTURL
	logLevel := opts.logLevel

	if envPath != "" {
		if err := envfile.Load(envPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("load provider environment: %w", err)
			}
		}
	}
	runtimeConfig, err := runtimeconfig.StageStatic(configPath, appDir)
	if err != nil {
		return err
	}
	staged, err := os.ReadFile(runtimeConfig)
	if err != nil {
		return fmt.Errorf("read staged config: %w", err)
	}
	if err := runtimeconfig.ReconcileVirtualKeyIDs(filepath.Join(appDir, "config.db"), staged); err != nil {
		return err
	}
	if err := bifrostserver.RegisterStaticPlugin(routerplugin.Name, func(_ context.Context, raw any, _ *lib.Config) (schemas.BasePlugin, error) {
		return routerplugin.New(raw)
	}); err != nil {
		return err
	}

	logger := bifrost.NewDefaultLogger(schemas.LogLevel(logLevel))
	lib.SetLogger(logger)
	bifrostserver.SetLogger(logger)
	handlers.SetLogger(logger)

	core := bifrostserver.NewBifrostHTTPServer(version, uiContent)
	core.Host = coreHost
	core.Port = corePort
	core.AppDir = appDir
	core.LogLevel = logLevel
	if err := core.Bootstrap(context.Background()); err != nil {
		return fmt.Errorf("bootstrap Bifrost: %w", err)
	}

	coreErr := make(chan error, 1)
	go func() { coreErr <- core.Start() }()
	coreURL := "http://" + coreHost + ":" + corePort
	if err := waitForHealth(coreURL+"/health", coreErr, 30*time.Second); err != nil {
		return err
	}

	cfg, err := gateway.LoadConfig(runtimeConfig)
	if err != nil {
		return err
	}
	public, err := gateway.NewServer(cfg, gateway.ServerOptions{
		Addr:       addr,
		BifrostURL: coreURL,
		ChatGPTURL: chatGPTURL,
	})
	if err != nil {
		return err
	}
	gatewayErr := make(chan error, 1)
	go func() {
		fmt.Printf("Bifrost Model Router listening on http://%s\n", public.Addr)
		err := public.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		gatewayErr <- err
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case <-signals:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := public.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown gateway: %w", err)
		}
		select {
		case err := <-coreErr:
			return err
		case <-shutdownCtx.Done():
			return fmt.Errorf("Bifrost shutdown timed out: %w", shutdownCtx.Err())
		}
	case err := <-coreErr:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = public.Shutdown(shutdownCtx)
		return err
	case err := <-gatewayErr:
		if process, findErr := os.FindProcess(os.Getpid()); findErr == nil {
			_ = process.Signal(os.Interrupt)
		}
		return err
	}
}

type options struct {
	configPath string
	envPath    string
	appDir     string
}

func defaultOptions() (options, error) {
	configPath, err := platformpaths.ConfigFile()
	if err != nil {
		return options{}, err
	}
	envPath, err := platformpaths.ProviderEnvFile()
	if err != nil {
		return options{}, err
	}
	stateDir, err := platformpaths.StateDir()
	if err != nil {
		return options{}, err
	}
	return options{configPath: configPath, envPath: envPath, appDir: filepath.Join(stateDir, "data")}, nil
}

func waitForHealth(url string, coreErr <-chan error, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	for {
		select {
		case err := <-coreErr:
			if err == nil {
				return fmt.Errorf("Bifrost stopped before becoming healthy")
			}
			return fmt.Errorf("Bifrost stopped before becoming healthy: %w", err)
		case <-deadline.C:
			return fmt.Errorf("Bifrost did not become healthy within %s", timeout)
		case <-ticker.C:
			response, err := client.Get(url)
			if err == nil {
				response.Body.Close()
				if response.StatusCode >= 200 && response.StatusCode < 300 {
					return nil
				}
			}
		}
	}
}
