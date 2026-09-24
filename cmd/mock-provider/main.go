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
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]any{
			{"id": p.mode + "-model", "object": "model", "owned_by": "mock"},
			{"id": p.mode + "-new-model", "object": "model", "owned_by": "mock"},
		}})
		return
	}
	var body struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid JSON"}})
		return
	}
	offersWebSearch, answered := false, ""
	for _, tool := range body.Tools {
		offersWebSearch = offersWebSearch || tool.Function.Name == "web_search"
	}
	for _, message := range body.Messages {
		if message.Role == "tool" {
			answered = fmt.Sprint(message.Content)
		}
	}
	if body.Stream {
		switch {
		case p.mode == "native" && strings.HasSuffix(r.URL.Path, "/responses"):
			writeNativeStream(w, body.Model)
		case p.mode == "chat" && strings.HasSuffix(r.URL.Path, "/chat/completions") && offersWebSearch && answered == "":
			writeChatToolCallStream(w, body.Model, "web_search", `{"query":"mock query"}`)
		case p.mode == "chat" && strings.HasSuffix(r.URL.Path, "/chat/completions") && answered != "":
			writeChatTextStream(w, body.Model, "tool said: "+answered)
		case p.mode == "chat" && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			writeChatStream(w, body.Model)
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": fmt.Sprintf("unexpected streaming %s path %s", p.mode, r.URL.Path)}})
		}
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

func writeNativeStream(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	response := nativeResponse(model)
	writeSSE(w, "response.created", map[string]any{
		"type": "response.created", "sequence_number": 0, "response": map[string]any{
			"id": "resp_native_stream", "object": "response", "created_at": 1, "model": model, "status": "in_progress", "output": []any{},
		},
	})
	writeSSE(w, "response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "sequence_number": 1, "item_id": "msg_native_stream", "output_index": 0, "content_index": 0, "delta": "native stream ok",
	})
	response["id"] = "resp_native_stream"
	writeSSE(w, "response.completed", map[string]any{
		"type": "response.completed", "sequence_number": 2, "response": response,
	})
}

func writeChatStream(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_stream", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "polyfill "}, "finish_reason": nil}},
	})
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_stream", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "stream ok"}, "finish_reason": nil}},
	})
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_stream", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeChatToolCallStream(w http.ResponseWriter, model, name, arguments string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_tool", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []map[string]any{{
			"index": 0, "id": "call_mock", "type": "function", "function": map[string]any{"name": name, "arguments": ""},
		}}}, "finish_reason": nil}},
	})
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_tool", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
			"index": 0, "function": map[string]any{"arguments": arguments},
		}}}, "finish_reason": nil}},
	})
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_tool", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeChatTextStream(w http.ResponseWriter, model, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_text", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}},
	})
	writeSSE(w, "", map[string]any{
		"id": "chatcmpl_text", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeSSE(w http.ResponseWriter, event string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
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
