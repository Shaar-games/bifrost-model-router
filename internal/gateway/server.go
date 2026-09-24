package gateway

import (
	"net/http"
	"time"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

type ServerOptions struct {
	Addr            string
	BifrostURL      string
	ChatGPTURL      string
	ShutdownTimeout time.Duration
}

func NewServer(cfg config.Config, options ServerOptions) (*http.Server, error) {
	handler, err := New(cfg, options.BifrostURL, options.ChatGPTURL)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              options.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}, nil
}
