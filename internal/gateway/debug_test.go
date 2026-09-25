package gateway

import (
	"strings"
	"testing"
)

func TestErrorSummaryOmitsResponseBody(t *testing.T) {
	body := []byte(`{"error":{"type":"service_unavailable","code":"503","message":"busy"},"extra_fields":{"prompt":"secret conversation"}}`)
	got := errorSummary(body)
	if strings.Contains(got, "secret") || strings.Contains(got, "extra_fields") {
		t.Fatalf("summary leaked the body: %s", got)
	}
	if !strings.Contains(got, "type=service_unavailable") || !strings.Contains(got, "code=503") || !strings.Contains(got, "message=\"busy\"") {
		t.Fatalf("summary = %s", got)
	}
}

func TestDebugResponseDropsOversizedEvents(t *testing.T) {
	writer := &debugResponseWriter{}
	writer.consumeEvents([]byte(strings.Repeat("x", maxDebugEventBytes+10)))
	if len(writer.buf) != 0 || !writer.skip {
		t.Fatalf("oversized event buffered: len=%d skip=%t", len(writer.buf), writer.skip)
	}
	writer.consumeEvents([]byte("\n\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"name\":\"exec_command\"}}\n\n"))
	if writer.skip || len(writer.buf) != 0 {
		t.Fatalf("writer did not resume after the oversized event: skip=%t buf=%q", writer.skip, writer.buf)
	}
}
