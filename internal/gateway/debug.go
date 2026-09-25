package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

// debugToolsEnabled logs only tool shapes, input item types, error codes and
// returned item types. It never logs prompts, arguments, outputs or headers.
// It is read lazily because the server loads providers.env into the environment
// after package initialization.
var debugToolsEnabled = sync.OnceValue(func() bool { return os.Getenv("ROUTER_DEBUG_TOOLS") != "" })

const maxDebugEventBytes = 4096

// debugErrorWriter keeps only a bounded prefix of a non-2xx body so the log can
// report the error code without retaining the rest of the response.
type debugErrorWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (w *debugErrorWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *debugErrorWriter) Write(data []byte) (int, error) {
	if w.status >= 400 && len(w.body) < 2048 {
		w.body = append(w.body, data[:min(len(data), 2048-len(w.body))]...)
	}
	return w.ResponseWriter.Write(data)
}

func (w *debugErrorWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func withDebugErrors(path string, w http.ResponseWriter, serve func(http.ResponseWriter)) {
	if !debugToolsEnabled() {
		serve(w)
		return
	}
	recorder := &debugErrorWriter{ResponseWriter: w, status: http.StatusOK}
	serve(recorder)
	if recorder.status >= 400 {
		log.Printf("router-debug %s error status=%d %s", path, recorder.status, errorSummary(recorder.body))
	}
}

func errorSummary(body []byte) string {
	var decoded struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return "unparsed=true"
	}
	message := decoded.Error.Message
	if len(message) > 160 {
		message = message[:160]
	}
	return fmt.Sprintf("type=%s code=%s message=%q", decoded.Error.Type, decoded.Error.Code, message)
}

// debugResponseWriter logs the function calls a streamed response returns:
// type, name and namespace only, never arguments or text.
type debugResponseWriter struct {
	http.ResponseWriter
	buf  []byte
	skip bool
}

func (w *debugResponseWriter) Write(data []byte) (int, error) {
	if debugToolsEnabled() {
		w.consumeEvents(data)
	}
	return w.ResponseWriter.Write(data)
}

// consumeEvents splits SSE events. An event larger than maxDebugEventBytes is
// discarded instead of being buffered, so a payload such as an image result
// cannot grow the log buffer.
func (w *debugResponseWriter) consumeEvents(data []byte) {
	for len(data) > 0 {
		if w.skip {
			end := bytes.Index(data, []byte("\n\n"))
			if end < 0 {
				return
			}
			w.skip = false
			data = data[end+2:]
			continue
		}
		room := maxDebugEventBytes - len(w.buf)
		if room <= 0 {
			w.buf = nil
			w.skip = true
			continue
		}
		take := min(len(data), room)
		w.buf = append(w.buf, data[:take]...)
		data = data[take:]
		for {
			end := bytes.Index(w.buf, []byte("\n\n"))
			if end < 0 {
				break
			}
			logReturnedCall(w.buf[:end])
			w.buf = w.buf[end+2:]
		}
		if len(w.buf) >= maxDebugEventBytes {
			w.buf = nil
			w.skip = true
		}
	}
}

func (w *debugResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func logReturnedCall(event []byte) {
	line := event
	if data := bytes.Index(event, []byte("data:")); data >= 0 {
		line = event[data+len("data:"):]
	}
	var decoded struct {
		Type string `json:"type"`
		Item struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"item"`
	}
	if json.Unmarshal(line, &decoded) != nil || decoded.Type != "response.output_item.done" {
		return
	}
	log.Printf("router-debug returned-item: type=%s name=%s namespace=%s", decoded.Item.Type, decoded.Item.Name, decoded.Item.Namespace)
}

func withDebugResponse(w http.ResponseWriter, serve func(http.ResponseWriter)) {
	if !debugToolsEnabled() {
		serve(w)
		return
	}
	serve(&debugResponseWriter{ResponseWriter: w})
}

func logToolShapes(stage string, body []byte) {
	if !debugToolsEnabled() {
		return
	}
	var envelope struct {
		Model      string            `json:"model"`
		Tools      []json.RawMessage `json:"tools"`
		ToolChoice json.RawMessage   `json:"tool_choice"`
		Input      json.RawMessage   `json:"input"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		log.Printf("router-debug %s: invalid JSON: %v", stage, err)
		return
	}
	shapes := make([]string, 0, len(envelope.Tools))
	for _, raw := range envelope.Tools {
		shapes = append(shapes, describeTool(raw))
	}
	log.Printf("router-debug %s: model=%s tool_choice=%s tools(%d)=[%s] input=%s",
		stage, envelope.Model, compactChoice(envelope.ToolChoice), len(shapes), strings.Join(shapes, "; "), describeInput(envelope.Input))
}

func describeTool(raw json.RawMessage) string {
	var tool struct {
		Type         string            `json:"type"`
		Name         string            `json:"name"`
		Execution    string            `json:"execution"`
		DeferLoading *bool             `json:"defer_loading"`
		Tools        []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &tool) != nil {
		return "?"
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	fields := make([]string, 0, len(keys))
	for key := range keys {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	desc := tool.Type
	if tool.Name != "" {
		desc += ":" + tool.Name
	}
	if tool.Execution != "" {
		desc += " execution=" + tool.Execution
	}
	if tool.DeferLoading != nil {
		desc += fmt.Sprintf(" defer_loading=%t", *tool.DeferLoading)
	}
	desc += " keys=" + strings.Join(fields, ",")
	if len(tool.Tools) > 0 {
		children := make([]string, 0, len(tool.Tools))
		for _, child := range tool.Tools {
			children = append(children, describeTool(child))
		}
		desc += " {" + strings.Join(children, ", ") + "}"
	}
	return desc
}

func describeInput(raw json.RawMessage) string {
	var items []struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return "text"
	}
	counts := map[string]int{}
	for _, item := range items {
		key := item.Type
		if key == "" || key == "message" {
			key = "message/" + item.Role
		}
		counts[key]++
	}
	parts := make([]string, 0, len(counts))
	for key, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", key, count))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

func compactChoice(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) == nil && choice.Type != "" {
		return choice.Type + ":" + choice.Name
	}
	return strings.Trim(string(raw), `"`)
}
