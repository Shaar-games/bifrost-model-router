package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func bridgeConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.HostedToolPolicy = config.HostedToolStrip
	cfg.HostedToolOverrides = map[string]config.HostedToolPolicy{
		"web_search": config.HostedToolBridge, "image_generation": config.HostedToolBridge,
	}
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func writeTestSSE(w http.ResponseWriter, events ...map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	}
}

// bridgeUpstream fakes the Bifrost core for the polyfilled model and the
// ChatGPT passthrough for the fallback model.
type bridgeUpstream struct {
	t            *testing.T
	mu           sync.Mutex
	polyfill     [][]json.RawMessage
	polyfillTool []string
	hosted       []map[string]any
	firstTurn    func(w http.ResponseWriter)
}

func (u *bridgeUpstream) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	defer u.mu.Unlock()
	switch req.URL.Path {
	case chatGPTResponsesPath:
		if req.Header.Get("Authorization") != "Bearer test" {
			u.t.Error("the fallback call lost the Codex OpenAI credential")
		}
		var hosted map[string]any
		_ = json.Unmarshal(body, &hosted)
		u.hosted = append(u.hosted, hosted)
		tools := hosted["tools"].([]any)
		switch tools[0].(map[string]any)["type"] {
		case "web_search":
			writeTestSSE(w,
				map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "message", "content": []any{map[string]any{
					"type": "output_text", "text": "Go 1.27 is current.",
					"annotations": []any{map[string]any{"type": "url_citation", "url": "https://go.dev/doc", "title": "Go docs"}},
				}}}},
				map[string]any{"type": "response.completed", "response": map[string]any{"output": []any{}}},
			)
		case "image_generation":
			writeTestSSE(w, map[string]any{"type": "response.completed", "response": map[string]any{"output": []any{
				map[string]any{"type": "image_generation_call", "result": "aW1hZ2U=", "revised_prompt": "a mark"},
			}}})
		}
	case "/v1/responses":
		var request struct {
			Model string            `json:"model"`
			Input []json.RawMessage `json:"input"`
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			u.t.Fatal(err)
		}
		if request.Model != "managed/text-model" {
			u.t.Errorf("polyfill model = %q", request.Model)
		}
		for _, tool := range request.Tools {
			u.polyfillTool = append(u.polyfillTool, tool.Type+":"+tool.Name)
		}
		u.polyfill = append(u.polyfill, request.Input)
		if len(u.polyfill) == 1 {
			u.firstTurn(w)
			return
		}
		writeTestSSE(w,
			map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_2"}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "msg_2", "type": "message"}},
			map[string]any{"type": "response.output_text.delta", "output_index": 0, "item_id": "msg_2", "delta": "Answer"},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": "msg_2", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Answer"}}}},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_2", "status": "completed", "output": []any{}, "usage": map[string]any{"total_tokens": 9}}},
		)
	default:
		u.t.Errorf("unexpected path %q", req.URL.Path)
	}
}

func bridgedCallTurn(name, arguments string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		call := map[string]any{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": name}
		done := map[string]any{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": name, "arguments": arguments}
		writeTestSSE(w,
			map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}},
			map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "rs_1", "type": "reasoning"}},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": "rs_1", "type": "reasoning", "summary": []any{}}},
			map[string]any{"type": "response.output_item.added", "output_index": 1, "item": call},
			map[string]any{"type": "response.function_call_arguments.delta", "output_index": 1, "item_id": "fc_1", "delta": arguments},
			map[string]any{"type": "response.output_item.done", "output_index": 1, "item": done},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_1", "status": "completed", "output": []any{}}},
		)
	}
}

type sseEvent struct {
	Type        string          `json:"type"`
	OutputIndex *int            `json:"output_index"`
	Item        json.RawMessage `json:"item"`
	Response    struct {
		ID     string            `json:"id"`
		Output []json.RawMessage `json:"output"`
	} `json:"response"`
}

func parseTestSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event sseEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func serveBridge(t *testing.T, upstream *bridgeUpstream, body string) *httptest.ResponseRecorder {
	t.Helper()
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	handler, err := New(bridgeConfig(t), server.URL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", stringsReader(body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("x-bf-vk", "sk-bf-test")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func TestBridgedWebSearchStreamsOneCodexResponse(t *testing.T) {
	upstream := &bridgeUpstream{t: t, firstTurn: bridgedCallTurn("web_search", `{"query":"latest Go"}`)}
	resp := serveBridge(t, upstream, `{"model":"managed/text-model","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"web_search_call","id":"ws_old","status":"completed"}],"tools":[{"type":"function","name":"shell"},{"type":"web_search"}]}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.Code, resp.Body.String())
	}

	if strings.Join(upstream.polyfillTool, ",") != "function:shell,function:web_search,function:shell,function:web_search" {
		t.Fatalf("polyfill tools = %v", upstream.polyfillTool)
	}
	if len(upstream.polyfill[0]) != 1 {
		t.Fatalf("replayed hosted item reached the polyfill: %s", upstream.polyfill[0])
	}
	continued := string(upstream.polyfill[1][len(upstream.polyfill[1])-1])
	if !strings.Contains(continued, `"function_call_output"`) || !strings.Contains(continued, `"call_id":"call_1"`) ||
		!strings.Contains(continued, "Go 1.27 is current.") || !strings.Contains(continued, "Go docs - https://go.dev/doc") {
		t.Fatalf("continued input = %s", continued)
	}
	if len(upstream.hosted) != 1 || upstream.hosted[0]["model"] != "luna" || upstream.hosted[0]["tool_choice"] != "required" {
		t.Fatalf("hosted call = %#v", upstream.hosted)
	}

	events := parseTestSSE(t, resp.Body.String())
	var kinds []string
	for _, event := range events {
		if strings.Contains(string(event.Item), "function_call") || event.Type == "response.function_call_arguments.delta" {
			t.Fatalf("bridged call leaked to Codex: %#v", event)
		}
		kinds = append(kinds, event.Type)
	}
	want := "response.created,response.output_item.added,response.output_item.done," +
		"response.output_item.added,response.output_item.done," +
		"response.output_item.added,response.output_text.delta,response.output_item.done,response.completed"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("events = %v", kinds)
	}
	if *events[3].OutputIndex != 1 || *events[5].OutputIndex != 2 || *events[6].OutputIndex != 2 {
		t.Fatalf("output indexes were not renumbered: %d %d %d", *events[3].OutputIndex, *events[5].OutputIndex, *events[6].OutputIndex)
	}
	if !strings.Contains(string(events[4].Item), `"web_search_call"`) || !strings.Contains(string(events[4].Item), `"completed"`) {
		t.Fatalf("web search item = %s", events[4].Item)
	}
	final := events[len(events)-1].Response
	if final.ID != "resp_1" || len(final.Output) != 3 {
		t.Fatalf("final response id = %q output = %d", final.ID, len(final.Output))
	}
}

func TestBridgedImageGenerationEmitsImageItem(t *testing.T) {
	upstream := &bridgeUpstream{t: t, firstTurn: bridgedCallTurn("image_generation", `{"prompt":"draw a mark","size":"1024x1024"}`)}
	resp := serveBridge(t, upstream, `{"model":"managed/text-model","stream":true,"input":"draw","tools":[{"type":"image_generation"}]}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.Code, resp.Body.String())
	}
	tool := upstream.hosted[0]["tools"].([]any)[0].(map[string]any)
	if tool["size"] != "1024x1024" {
		t.Fatalf("image tool = %#v", tool)
	}
	if !strings.Contains(resp.Body.String(), `"type":"image_generation_call"`) || !strings.Contains(resp.Body.String(), `"result":"aW1hZ2U="`) {
		t.Fatalf("image item missing: %s", resp.Body.String())
	}
	if !strings.Contains(string(upstream.polyfill[1][len(upstream.polyfill[1])-1]), "displayed to the user") {
		t.Fatalf("image result = %s", upstream.polyfill[1])
	}
}

func TestBridgedWebSearchWithoutStreaming(t *testing.T) {
	turns := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == chatGPTResponsesPath {
			writeTestSSE(w, map[string]any{"type": "response.completed", "response": map[string]any{"output": []any{
				map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "found"}}},
			}}})
			return
		}
		turns++
		w.Header().Set("Content-Type", "application/json")
		if turns == 1 {
			_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"function_call","call_id":"call_1","name":"web_search","arguments":"{\"query\":\"q\"}"}]}`))
			return
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"output":"found"`) {
			t.Errorf("continued request = %s", body)
		}
		_, _ = w.Write([]byte(`{"id":"resp_2","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	defer server.Close()
	handler, err := New(bridgeConfig(t), server.URL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", stringsReader(`{"model":"managed/text-model","input":"hi","tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("x-bf-vk", "sk-bf-test")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	var got struct {
		ID     string `json:"id"`
		Output []struct {
			Type string `json:"type"`
		} `json:"output"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body = %s", resp.Body.String())
	}
	if turns != 2 || got.ID != "resp_1" || len(got.Output) != 2 || got.Output[0].Type != "web_search_call" || got.Output[1].Type != "message" {
		t.Fatalf("turns = %d response = %s", turns, resp.Body.String())
	}
}

func TestBridgedCallsAreDroppedWhenCodexMustRunOtherCalls(t *testing.T) {
	upstream := &bridgeUpstream{t: t, firstTurn: func(w http.ResponseWriter) {
		writeTestSSE(w,
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": "fc_1", "type": "function_call", "call_id": "call_1", "name": "web_search", "arguments": `{"query":"x"}`}},
			map[string]any{"type": "response.output_item.done", "output_index": 1, "item": map[string]any{"id": "fc_2", "type": "function_call", "call_id": "call_2", "name": "shell", "arguments": `{}`}},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_1", "output": []any{}}},
		)
	}}
	resp := serveBridge(t, upstream, `{"model":"managed/text-model","stream":true,"input":"hi","tools":[{"type":"function","name":"shell"},{"type":"web_search"}]}`)
	if len(upstream.polyfill) != 1 || len(upstream.hosted) != 0 {
		t.Fatalf("turns = %d hosted = %d", len(upstream.polyfill), len(upstream.hosted))
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"name":"shell"`) || strings.Contains(body, `"name":"web_search"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestBridgedWebSearchWithoutCodexLoginReportsToolFailure(t *testing.T) {
	upstream := &bridgeUpstream{t: t, firstTurn: bridgedCallTurn("web_search", `{"query":"latest Go"}`)}
	server := httptest.NewServer(upstream)
	defer server.Close()
	handler, err := New(bridgeConfig(t), server.URL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", stringsReader(`{"model":"managed/text-model","stream":true,"input":"hi","tools":[{"type":"web_search"}]}`))
	req.Header.Set("x-bf-vk", "sk-bf-test")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if len(upstream.hosted) != 0 {
		t.Fatal("the fallback was called without a Codex login")
	}
	if len(upstream.polyfill) != 2 || !strings.Contains(string(upstream.polyfill[1][len(upstream.polyfill[1])-1]), "web_search failed") {
		t.Fatalf("polyfill turns = %s", upstream.polyfill)
	}
}
