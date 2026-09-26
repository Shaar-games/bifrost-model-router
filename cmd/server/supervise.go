package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/applyinnovations/bifrost-model-router/internal/configwatch"
)

const supervisedEnv = "BIFROST_ROUTER_SUPERVISED"

func supervise(configPath, envPath string) error {
	paths := []string{configPath}
	if envPath != "" {
		paths = append(paths, envPath)
	}
	baseline, err := configwatch.Fingerprint(paths)
	if err != nil {
		return err
	}
	state := configwatch.State{Baseline: baseline}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve server binary: %w", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	var (
		command    *exec.Cmd
		waitResult chan error
		restarting bool
	)
	start := func() error {
		command = exec.Command(executable, os.Args[1:]...)
		command.Env = append(os.Environ(), supervisedEnv+"=1")
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			return fmt.Errorf("start server: %w", err)
		}
		waitResult = make(chan error, 1)
		go func(process *exec.Cmd) {
			waitResult <- process.Wait()
		}(command)
		return nil
	}
	stop := func() {
		if command == nil || command.Process == nil {
			return
		}
		_ = command.Process.Kill()
	}
	if err := start(); err != nil {
		return err
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-signals:
			stop()
			return nil
		case err := <-waitResult:
			command = nil
			if restarting {
				restarting = false
				time.Sleep(200 * time.Millisecond)
				if err := start(); err != nil {
					fmt.Fprintf(os.Stderr, "bifrost-model-router: restart failed: %v\n", err)
				}
				continue
			}
			if err == nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "bifrost-model-router: server stopped (%v); waiting for a configuration change\n", err)
		case now := <-ticker.C:
			fingerprint, err := configwatch.Fingerprint(paths)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bifrost-model-router: %v\n", err)
				continue
			}
			if !state.Observe(now, fingerprint, configwatch.QuietWindow) {
				continue
			}
			fmt.Fprintln(os.Stderr, "bifrost-model-router: configuration changed, restarting")
			if command == nil {
				if err := start(); err != nil {
					fmt.Fprintf(os.Stderr, "bifrost-model-router: restart failed: %v\n", err)
				}
				continue
			}
			restarting = true
			stop()
		}
	}
}
