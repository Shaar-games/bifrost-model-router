package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: config-check <router.yaml>")
		os.Exit(2)
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		fatal(err)
	}
	defer file.Close()
	cfg, err := config.Decode(file)
	if err != nil {
		fatal(err)
	}
	output, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(output))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "config-check:", err)
	os.Exit(1)
}
