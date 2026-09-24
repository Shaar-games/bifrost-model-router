package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/applyinnovations/bifrost-model-router/internal/gateway"
)

func main() {
	configPath := env("BIFROST_CONFIG_FILE", "/etc/bifrost/config.json")
	cfg, err := gateway.LoadConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}
	server, err := gateway.NewServer(cfg, gateway.ServerOptions{
		Addr:       env("GATEWAY_ADDR", "127.0.0.1:8082"),
		BifrostURL: env("BIFROST_UPSTREAM_URL", "http://127.0.0.1:8080"),
		ChatGPTURL: env("CHATGPT_UPSTREAM_URL", "https://chatgpt.com"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Codex dispatch gateway listening on %s\n", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
