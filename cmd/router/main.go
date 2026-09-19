package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/applyinnovations/bifrost-model-router/internal/codexprofile"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "version":
		fmt.Printf("bifrost-model-router %s go=%s\n", version, goVersion())
	case "profile":
		profile(os.Args[2:])
	default:
		usage()
	}
}

func profile(args []string) {
	if len(args) == 0 {
		usage()
	}
	command := args[0]
	flags := flag.NewFlagSet("router profile "+command, flag.ExitOnError)
	path := flags.String("config", defaultConfigPath(), "Codex user config path")
	provider := flags.String("provider", "bifrost-router", "Codex provider name")
	baseURL := flags.String("base-url", "http://127.0.0.1:8080/v1", "router API base URL")
	if err := flags.Parse(args[1:]); err != nil {
		fatal(err)
	}
	options := codexprofile.Options{Provider: *provider, BaseURL: *baseURL}
	switch command {
	case "render":
		block, err := codexprofile.Render(options)
		if err != nil {
			fatal(err)
		}
		fmt.Print(block)
	case "install":
		backup, err := codexprofile.Install(*path, options, time.Now())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("installed Codex provider in %s\n", *path)
		if backup != "" {
			fmt.Printf("backup: %s\n", backup)
		}
	case "uninstall":
		backup, found, err := codexprofile.Uninstall(*path, time.Now())
		if err != nil {
			fatal(err)
		}
		if !found {
			fmt.Println("managed Codex provider was not installed")
			return
		}
		fmt.Printf("removed managed Codex provider; backup: %s\n", backup)
	case "rollback":
		if flags.NArg() != 1 {
			fatal(fmt.Errorf("rollback requires one backup path"))
		}
		if err := codexprofile.Restore(*path, flags.Arg(0)); err != nil {
			fatal(err)
		}
		fmt.Printf("restored %s from %s\n", *path, flags.Arg(0))
	default:
		usage()
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codex", "config.toml")
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func goVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return info.GoVersion
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: router version | router profile <render|install|uninstall|rollback> [options]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "router:", err)
	os.Exit(1)
}
