package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18101", "listen address")
	expectedToken := flag.String("expected-token", "", "required bearer token")
	mode := flag.String("mode", "native", "native or chat")
	flag.Parse()

	handler := &provider{expectedToken: *expectedToken, mode: *mode}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("mock provider mode=%s listening on %s", *mode, *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

type provider struct {
	expectedToken string
	mode          string
}

func (p *provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+p.expectedToken {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "credential canary mismatch"}})
		return
	}
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]any{{"id": p.mode + "-model", "object": "model", "owned_by": "mock"}}})
		return
	}
	var body struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid JSON"}})
		return
	}
	if body.Stream {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "stream fixture not requested in this test"}})
		return
	}
	switch {
	case p.mode == "native" && strings.HasSuffix(r.URL.Path, "/responses"):
		writeJSON(w, http.StatusOK, nativeResponse(body.Model))
	case p.mode == "chat" && strings.HasSuffix(r.URL.Path, "/chat/completions"):
		writeJSON(w, http.StatusOK, chatResponse(body.Model))
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": fmt.Sprintf("unexpected %s path %s", p.mode, r.URL.Path)}})
	}
}

func nativeResponse(model string) map[string]any {
	return map[string]any{
		"id": "resp_native", "object": "response", "created_at": 1, "model": model, "status": "completed",
		"output": []map[string]any{{
			"id": "msg_native", "type": "message", "role": "assistant", "status": "completed",
			"content": []map[string]any{{"type": "output_text", "text": "native ok", "annotations": []any{}}},
		}},
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
	}
}

func chatResponse(model string) map[string]any {
	return map[string]any{
		"id": "chatcmpl_polyfill", "object": "chat.completion", "created": 1, "model": model,
		"choices": []map[string]any{{
			"index": 0, "message": map[string]any{"role": "assistant", "content": "polyfill ok"}, "finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
