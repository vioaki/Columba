package nativeusage

import (
	"strings"
	"testing"
)

func TestUsageProtocolCountersRemainOnTheirNativeBasis(t *testing.T) {
	for _, tc := range []struct {
		protocol, body                  string
		input, output, cache, reasoning int64
	}{
		{"openai", `{"usage":{"prompt_tokens":100,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":10}}}`, 100, 30, 40, 10},
		{"responses", `{"response":{"id":"resp_x","usage":{"input_tokens":100,"output_tokens":30,"input_tokens_details":{"cached_tokens":40},"output_tokens_details":{"reasoning_tokens":10}}}}`, 100, 30, 40, 10},
		{"claude", `{"usage":{"input_tokens":100,"output_tokens":30,"cache_read_input_tokens":40,"cache_creation_input_tokens":15}}`, 100, 30, 40, 0},
		{"gemini", `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":30,"cachedContentTokenCount":40,"thoughtsTokenCount":10}}`, 100, 30, 40, 10},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			o := Observer{Protocol: tc.protocol}
			o.Write([]byte(tc.body))
			o.Finish()
			u := o.Usage
			if u.Input == nil || *u.Input != tc.input || u.Output == nil || *u.Output != tc.output || u.CacheRead == nil || *u.CacheRead != tc.cache {
				t.Fatal(u)
			}
			if tc.reasoning > 0 && (u.Reasoning == nil || *u.Reasoning != tc.reasoning) {
				t.Fatal(u)
			}
		})
	}
}

func TestSSETailUsageChunkBoundariesAndDuplicateEvents(t *testing.T) {
	input := ": heartbeat\r\nevent: message_start\r\ndata: {\"message\":{\"id\":\"msg_x\",\"usage\":{\"input_tokens\":50,\"cache_read_input_tokens\":0}}}\r\n\r\n"
	delta := "data: {\"usage\":{\"output_tokens\":20}}\n\n"
	input += delta + delta + "data: [DONE]\n\n"
	for _, size := range []int{1, 7, 41, len(input)} {
		o := Observer{Protocol: "claude", SSE: true}
		for start := 0; start < len(input); start += size {
			o.Write([]byte(input[start:min(start+size, len(input))]))
		}
		o.Finish()
		if o.Usage.Input == nil || *o.Usage.Input != 50 || o.Usage.Output == nil || *o.Usage.Output != 20 || o.Usage.CacheRead == nil || *o.Usage.CacheRead != 0 || len(o.IDs) != 1 {
			t.Fatal(size, o)
		}
	}
}

func TestUsageMissingInvalidAndOversizedStayDistinctFromZero(t *testing.T) {
	for _, body := range []string{`{"choices":[]}`, `{"usage":{"prompt_tokens":-1}}`, `{"usage":{"prompt_tokens":"bad"}}`} {
		o := Observer{Protocol: "openai"}
		o.Write([]byte(body))
		o.Finish()
		if o.Usage.Input != nil {
			t.Fatal("invented usage", o)
		}
	}
	o := Observer{Protocol: "openai", SSE: true}
	o.Write([]byte("data: " + strings.Repeat("x", MaxObservationBytes+1) + "\n\n"))
	o.Write([]byte("data: {\"usage\":{\"prompt_tokens\":0}}\n\n"))
	o.Finish()
	if len(o.Anomalies) == 0 || o.Usage.Input == nil || *o.Usage.Input != 0 {
		t.Fatal(o)
	}
}

func TestUncoveredFeesAndMultimodalUsage(t *testing.T) {
	for _, tc := range []struct {
		protocol, body     string
		uncovered, nonText bool
	}{
		{"responses", `{"response":{"output":[{"type":"web_search_call"}],"usage":{"input_tokens":20,"output_tokens":2}}}`, true, false},
		{"responses", `{"type":"response.output_item.added","item":{"type":"image_generation_call"}}`, true, false},
		{"responses", `{"type":"response.web_search_call.completed"}`, true, false},
		{"responses", `{"output":[{"type":"function_call"}],"usage":{"input_tokens":20,"output_tokens":2}}`, false, false},
		{"openai", `{"usage":{"prompt_tokens":20,"completion_tokens":2,"prompt_tokens_details":{"audio_tokens":10}}}`, false, true},
		{"gemini", `{"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":2,"promptTokensDetails":[{"modality":"AUDIO","tokenCount":10}]}}`, false, true},
		{"claude", `{"usage":{"input_tokens":20,"output_tokens":2,"server_tool_use":{"web_search_requests":0}}}`, false, false},
		{"claude", `{"usage":{"input_tokens":20,"output_tokens":2,"server_tool_use":{"web_search_requests":1}}}`, true, false},
	} {
		o := Observer{Protocol: tc.protocol}
		o.Write([]byte(tc.body))
		o.Finish()
		if o.Usage.Uncovered != tc.uncovered || o.Usage.NonText != tc.nonText {
			t.Fatalf("%s: %+v", tc.body, o.Usage)
		}
	}
}
