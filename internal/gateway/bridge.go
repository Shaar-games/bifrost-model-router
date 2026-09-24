package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxBridgeTurns bounds how many polyfilled-model turns one Codex request may
// spend on bridged tool calls before the last turn is returned as is.
const maxBridgeTurns = 8

const webSearchInstructions = "You are a web research tool used by another AI assistant. " +
	"Search the web for the query and answer with a concise, factual summary of the findings. " +
	"Cite the source URL for each fact."

// replayedHostedItems are output items the gateway synthesizes for bridged
// calls. Codex replays them as input, but a Chat Completions wire cannot.
var replayedHostedItems = map[string]bool{"web_search_call": true, "image_generation_call": true}

type bridgeCall struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type bridgeTurn struct {
	final      map[string]json.RawMessage
	finalType  string
	output     []json.RawMessage
	calls      []bridgeCall
	otherCalls bool
}

// bridgeSession runs one Codex request against a polyfilled model, executing
// calls to bridged hosted tools on the fallback model and continuing the
// polyfilled model with their results. Codex sees one response: the bridged
// function calls are hidden and replaced by native hosted-tool output items.
type bridgeSession struct {
	h          *Handler
	req        *http.Request
	w          http.ResponseWriter
	stream     bool
	bridges    map[string]bool
	started    bool
	sequence   int
	nextIndex  int
	responseID json.RawMessage
	visible    []json.RawMessage
}

func (h *Handler) serveBridgedResponses(w http.ResponseWriter, req *http.Request, body []byte, bridges []string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	input, err := inputItems(fields["input"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "input must be a string or an array of items")
		return
	}
	s := &bridgeSession{h: h, req: req, w: w, bridges: make(map[string]bool, len(bridges))}
	_ = json.Unmarshal(fields["stream"], &s.stream)
	for _, name := range bridges {
		s.bridges[name] = true
	}

	for turn := 0; ; turn++ {
		fields["input"], err = json.Marshal(input)
		if err != nil {
			s.fail(http.StatusInternalServerError, "invalid_request", "could not encode the bridged input")
			return
		}
		payload, err := json.Marshal(fields)
		if err != nil {
			s.fail(http.StatusInternalServerError, "invalid_request", "could not encode the bridged request")
			return
		}
		resp, err := s.post(payload)
		if err != nil {
			s.fail(http.StatusBadGateway, "upstream_unavailable", err.Error())
			return
		}
		if resp.StatusCode != http.StatusOK {
			s.relayError(resp)
			return
		}
		result, err := s.readTurn(resp, turn == 0)
		if err != nil {
			s.fail(http.StatusBadGateway, "upstream_invalid", err.Error())
			return
		}
		if len(result.calls) == 0 || result.otherCalls || turn+1 >= maxBridgeTurns {
			s.finish(result)
			return
		}
		input = append(input, result.output...)
		for _, call := range result.calls {
			input = append(input, s.execute(call))
		}
	}
}

func inputItems(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		item, err := json.Marshal(map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": text}},
		})
		return []json.RawMessage{item}, err
	}
	var items []json.RawMessage
	err := json.Unmarshal(raw, &items)
	return items, err
}

// dropReplayedHostedItems removes synthesized hosted-tool items from a
// polyfilled request's input. It returns the body unchanged when none exist.
func dropReplayedHostedItems(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if json.Unmarshal(fields["input"], &items) != nil {
		return body, nil
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &head) == nil && replayedHostedItems[head.Type] {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == len(items) {
		return body, nil
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	fields["input"] = encoded
	return json.Marshal(fields)
}

func (s *bridgeSession) post(payload []byte) (*http.Response, error) {
	target := *s.h.bifrostURL
	target.Path = "/v1/responses"
	target.RawQuery = s.req.URL.RawQuery
	out, err := http.NewRequestWithContext(s.req.Context(), http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	out.Header = s.req.Header.Clone()
	out.Header.Del("Content-Length")
	out.Header.Del("Accept-Encoding")
	out.Header.Set("Content-Type", "application/json")
	return s.h.streamClient.Do(out)
}

// classify reports whether an output item is a call to a bridged tool, and
// whether it is any function call at all.
func (s *bridgeSession) classify(item json.RawMessage) (call bridgeCall, itemID string, bridged, function bool) {
	var head struct {
		bridgeCall
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if json.Unmarshal(item, &head) != nil || head.Type != "function_call" {
		return bridgeCall{}, head.ID, false, false
	}
	return head.bridgeCall, head.ID, s.bridges[head.Name], true
}

func (s *bridgeSession) readTurn(resp *http.Response, first bool) (bridgeTurn, error) {
	defer resp.Body.Close()
	if !s.stream {
		return s.readJSONTurn(resp.Body, first)
	}
	result := bridgeTurn{}
	indexes := make(map[int]int)
	hiddenIndexes := make(map[int]bool)
	hiddenItems := make(map[string]bool)
	handle := func(data string) {
		if data == "" || data == "[DONE]" {
			return
		}
		var event map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &event) != nil {
			return
		}
		var kind, itemID string
		_ = json.Unmarshal(event["type"], &kind)
		_ = json.Unmarshal(event["item_id"], &itemID)
		var outputIndex int
		hasIndex := json.Unmarshal(event["output_index"], &outputIndex) == nil

		switch kind {
		case "response.created", "response.in_progress", "response.queued":
			if !first {
				return
			}
			if kind == "response.created" {
				var response struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(event["response"], &response) == nil {
					s.responseID = response.ID
				}
			}
		case "response.completed", "response.incomplete", "response.failed":
			result.finalType = kind
			_ = json.Unmarshal(event["response"], &result.final)
			return
		case "response.output_item.added", "response.output_item.done":
			call, id, bridged, function := s.classify(event["item"])
			done := kind == "response.output_item.done"
			if done {
				result.output = append(result.output, event["item"])
			}
			if bridged {
				hiddenItems[id] = true
				if hasIndex {
					hiddenIndexes[outputIndex] = true
				}
				if done {
					result.calls = append(result.calls, call)
				}
				return
			}
			if function {
				result.otherCalls = true
			}
			if done {
				s.visible = append(s.visible, event["item"])
			}
		}
		if hiddenItems[itemID] || (hasIndex && hiddenIndexes[outputIndex]) {
			return
		}
		if hasIndex {
			mapped, ok := indexes[outputIndex]
			if !ok {
				mapped = s.nextIndex
				s.nextIndex++
				indexes[outputIndex] = mapped
			}
			event["output_index"], _ = json.Marshal(mapped)
		}
		s.emit(kind, event)
	}

	reader := bufio.NewReaderSize(resp.Body, 64<<10)
	var data []string
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "" && len(data) > 0:
			handle(strings.Join(data, "\n"))
			data = nil
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
	}
	if len(data) > 0 {
		handle(strings.Join(data, "\n"))
	}
	if result.finalType == "" {
		return result, fmt.Errorf("the polyfilled model stream ended without a terminal event")
	}
	return result, nil
}

func (s *bridgeSession) readJSONTurn(body io.Reader, first bool) (bridgeTurn, error) {
	result := bridgeTurn{finalType: "response.completed"}
	if err := json.NewDecoder(io.LimitReader(body, 64<<20)).Decode(&result.final); err != nil {
		return result, err
	}
	if first {
		s.responseID = result.final["id"]
	}
	if err := json.Unmarshal(result.final["output"], &result.output); err != nil {
		return result, fmt.Errorf("the polyfilled model response has no output array")
	}
	for _, item := range result.output {
		call, _, bridged, function := s.classify(item)
		switch {
		case bridged:
			result.calls = append(result.calls, call)
		case function:
			result.otherCalls = true
			s.visible = append(s.visible, item)
		default:
			s.visible = append(s.visible, item)
		}
	}
	return result, nil
}

func (s *bridgeSession) emit(kind string, event map[string]json.RawMessage) {
	if !s.started {
		s.w.Header().Set("Content-Type", "text/event-stream")
		s.w.Header().Set("Cache-Control", "no-cache")
		s.w.WriteHeader(http.StatusOK)
		s.started = true
	}
	event["sequence_number"], _ = json.Marshal(s.sequence)
	s.sequence++
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", kind, data)
	if flusher, ok := s.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *bridgeSession) emitValue(kind string, fields map[string]any) {
	event := make(map[string]json.RawMessage, len(fields)+1)
	event["type"], _ = json.Marshal(kind)
	for name, value := range fields {
		event[name], _ = json.Marshal(value)
	}
	s.emit(kind, event)
}

func (s *bridgeSession) relayError(resp *http.Response) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if !s.started {
		s.w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		s.w.WriteHeader(resp.StatusCode)
		_, _ = s.w.Write(body)
		return
	}
	s.fail(resp.StatusCode, "upstream_error", strings.TrimSpace(string(body)))
}

func (s *bridgeSession) fail(status int, code, message string) {
	if !s.started {
		writeError(s.w, status, code, message)
		return
	}
	response := map[string]any{
		"status": "failed",
		"output": s.visible,
		"error":  map[string]string{"code": code, "message": message},
	}
	if s.responseID != nil {
		response["id"] = s.responseID
	}
	s.emitValue("response.failed", map[string]any{"response": response})
}

func (s *bridgeSession) finish(result bridgeTurn) {
	response := result.final
	if response == nil {
		response = make(map[string]json.RawMessage)
	}
	response["output"], _ = json.Marshal(s.visible)
	if s.visible == nil {
		response["output"] = json.RawMessage("[]")
	}
	if s.responseID != nil {
		response["id"] = s.responseID
	}
	if !s.stream {
		s.w.Header().Set("Content-Type", "application/json")
		s.w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(s.w).Encode(response)
		return
	}
	encoded, _ := json.Marshal(response)
	s.emit(result.finalType, map[string]json.RawMessage{
		"type":     json.RawMessage(fmt.Sprintf("%q", result.finalType)),
		"response": encoded,
	})
}

// startItem announces a synthesized output item and reserves its index.
func (s *bridgeSession) startItem(item map[string]any) int {
	index := s.nextIndex
	s.nextIndex++
	if s.stream {
		s.emitValue("response.output_item.added", map[string]any{"output_index": index, "item": item})
	}
	return index
}

func (s *bridgeSession) finishItem(index int, item map[string]any) {
	encoded, err := json.Marshal(item)
	if err != nil {
		return
	}
	s.visible = append(s.visible, encoded)
	if s.stream {
		s.emitValue("response.output_item.done", map[string]any{"output_index": index, "item": item})
	}
}

func (s *bridgeSession) execute(call bridgeCall) json.RawMessage {
	var output string
	switch {
	case !strings.HasPrefix(s.req.Header.Get("Authorization"), "Bearer "):
		output = call.Name + " failed: Codex OpenAI authentication is required for bridged tools"
	case call.Name == "web_search":
		output = s.webSearch(call)
	case call.Name == "image_generation":
		output = s.imageGeneration(call)
	default:
		output = fmt.Sprintf("%s is not a bridged tool", call.Name)
	}
	encoded, _ := json.Marshal(map[string]string{"type": "function_call_output", "call_id": call.CallID, "output": output})
	return encoded
}

func (s *bridgeSession) webSearch(call bridgeCall) string {
	var args struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(call.Arguments), &args)
	item := map[string]any{
		"id": "ws_" + call.CallID, "type": "web_search_call", "status": "in_progress",
		"action": map[string]any{"type": "search", "query": args.Query},
	}
	index := s.startItem(item)
	output, err := s.runWebSearch(args.Query)
	item["status"] = "completed"
	if err != nil {
		item["status"] = "failed"
		output = "web_search failed: " + err.Error()
	}
	s.finishItem(index, item)
	return output
}

func (s *bridgeSession) runWebSearch(query string) (string, error) {
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	upstream, err := s.h.runHostedTool(s.req, map[string]any{"type": "web_search"}, "required", webSearchInstructions, query)
	if err != nil {
		return "", err
	}
	if upstream.Code != http.StatusOK {
		return "", fmt.Errorf("OpenAI returned HTTP %d: %s", upstream.Code, errorMessage(upstream.Body.Bytes()))
	}
	text := webSearchFromSSE(upstream.Body.Bytes())
	if text == "" {
		return "", fmt.Errorf("OpenAI returned no search results")
	}
	return text, nil
}

func (s *bridgeSession) imageGeneration(call bridgeCall) string {
	var args struct {
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
	}
	_ = json.Unmarshal([]byte(call.Arguments), &args)
	id := "ig_" + call.CallID
	index := s.startItem(map[string]any{"id": id, "type": "image_generation_call", "status": "in_progress"})
	fail := func(reason string) string {
		s.finishItem(index, map[string]any{"id": id, "type": "image_generation_call", "status": "failed"})
		return "image_generation failed: " + reason
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return fail("prompt is required")
	}
	tool := map[string]any{"type": "image_generation"}
	if args.Size != "" && args.Size != "auto" {
		tool["size"] = args.Size
	}
	upstream, err := s.h.runHostedTool(s.req, tool, map[string]string{"type": "image_generation"}, "", args.Prompt)
	if err != nil {
		return fail(err.Error())
	}
	if upstream.Code != http.StatusOK {
		return fail(fmt.Sprintf("OpenAI returned HTTP %d: %s", upstream.Code, errorMessage(upstream.Body.Bytes())))
	}
	encoded, revised := imageFromSSE(upstream.Body.Bytes())
	if encoded == "" {
		return fail("OpenAI returned no image")
	}
	item := map[string]any{"id": id, "type": "image_generation_call", "status": "completed", "result": encoded}
	if revised != "" {
		item["revised_prompt"] = revised
	}
	s.finishItem(index, item)
	return "The image was generated and is displayed to the user."
}

// webSearchFromSSE extracts the answer text and cited sources of a hosted
// web search run.
func webSearchFromSSE(body []byte) string {
	type content struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Annotations []struct {
			Type  string `json:"type"`
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"annotations"`
	}
	type item struct {
		Type    string    `json:"type"`
		Content []content `json:"content"`
	}
	var messages []item
	var completed []item
	for _, block := range bytes.Split(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), []byte("\n\n")) {
		for _, line := range bytes.Split(block, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			var event struct {
				Type     string `json:"type"`
				Item     item   `json:"item"`
				Response struct {
					Output []item `json:"output"`
				} `json:"response"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &event) != nil {
				continue
			}
			if event.Type == "response.output_item.done" && event.Item.Type == "message" {
				messages = append(messages, event.Item)
			}
			if event.Type == "response.completed" {
				completed = event.Response.Output
			}
		}
	}
	if len(messages) == 0 {
		messages = completed
	}
	var text strings.Builder
	var sources []string
	seen := make(map[string]bool)
	for _, message := range messages {
		if message.Type != "message" {
			continue
		}
		for _, part := range message.Content {
			if part.Type != "output_text" {
				continue
			}
			if text.Len() > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(part.Text)
			for _, annotation := range part.Annotations {
				if annotation.Type != "url_citation" || annotation.URL == "" || seen[annotation.URL] {
					continue
				}
				seen[annotation.URL] = true
				source := annotation.URL
				if annotation.Title != "" {
					source = annotation.Title + " - " + annotation.URL
				}
				sources = append(sources, "- "+source)
			}
		}
	}
	if text.Len() > 0 && len(sources) > 0 {
		text.WriteString("\n\nSources:\n" + strings.Join(sources, "\n"))
	}
	return strings.TrimSpace(text.String())
}

func errorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error.Message != "" {
			return envelope.Error.Message
		}
		if envelope.Detail != "" {
			return envelope.Detail
		}
	}
	message := strings.TrimSpace(string(body))
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}
