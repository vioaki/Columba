// Package nativeusage observes wire bytes without changing their delivery.
package nativeusage

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Nil counters mean the provider did not report that quantity. Counts preserve
// each protocol's original accounting basis; the price calculator owns overlap.
type Usage struct {
	Input        *int64 `json:"input,omitempty"`
	Output       *int64 `json:"output,omitempty"`
	Total        *int64 `json:"total,omitempty"`
	CacheRead    *int64 `json:"cacheRead,omitempty"`
	CacheWrite   *int64 `json:"cacheWrite,omitempty"`
	CacheWrite5m *int64 `json:"cacheWrite5m,omitempty"`
	CacheWrite1h *int64 `json:"cacheWrite1h,omitempty"`
	Reasoning    *int64 `json:"reasoning,omitempty"`
	Uncovered    bool   `json:"uncovered,omitempty"`
	NonText      bool   `json:"nonText,omitempty"`
}

type Observer struct {
	Protocol  string
	SSE       bool
	Usage     Usage
	IDs       []string
	Anomalies []string
	buffer    []byte
	event     []byte
	skipping  bool
}

const MaxObservationBytes = 8 << 20

func (o *Observer) anomaly(message string) {
	for _, m := range o.Anomalies {
		if m == message {
			return
		}
	}
	if len(o.Anomalies) < 8 {
		o.Anomalies = append(o.Anomalies, message)
	}
}

func (o *Observer) Write(p []byte) {
	if !o.SSE {
		if len(o.buffer)+len(p) > MaxObservationBytes {
			o.buffer = nil
			o.skipping = true
			o.anomaly("response exceeds usage observation limit")
		}
		if !o.skipping {
			o.buffer = append(o.buffer, p...)
		}
		return
	}
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		if end < 0 {
			if len(o.buffer)+len(p) > MaxObservationBytes {
				o.buffer = nil
				o.skipping = true
				o.anomaly("SSE line exceeds usage observation limit")
			}
			if !o.skipping {
				o.buffer = append(o.buffer, p...)
			}
			return
		}
		if !o.skipping && len(o.buffer)+end <= MaxObservationBytes {
			o.buffer = append(o.buffer, p[:end]...)
			o.line(bytes.TrimSuffix(o.buffer, []byte{'\r'}))
		} else {
			o.anomaly("SSE line exceeds usage observation limit")
		}
		o.buffer = o.buffer[:0]
		o.skipping = false
		p = p[end+1:]
	}
}

func (o *Observer) line(line []byte) {
	if len(line) == 0 {
		if len(o.event) > 0 {
			o.observe(bytes.TrimSuffix(o.event, []byte{'\n'}))
			o.event = o.event[:0]
		}
		return
	}
	if bytes.HasPrefix(line, []byte("data:")) {
		data := bytes.TrimPrefix(line[5:], []byte{' '})
		if len(o.event)+len(data) > MaxObservationBytes {
			o.event = o.event[:0]
			o.anomaly("SSE event exceeds usage observation limit")
			return
		}
		o.event = append(o.event, data...)
		o.event = append(o.event, '\n')
	}
}

func (o *Observer) Finish() {
	if !o.SSE {
		if !o.skipping && len(o.buffer) > 0 {
			o.observe(o.buffer)
		}
	} else {
		if len(o.buffer) > 0 && !o.skipping {
			o.line(bytes.TrimSuffix(o.buffer, []byte{'\r'}))
		}
		o.line(nil)
	}
	o.buffer = nil
	o.event = nil
}

func (o *Observer) addID(id string) {
	if id == "" {
		return
	}
	for _, old := range o.IDs {
		if old == id {
			return
		}
	}
	if len(o.IDs) < 8 {
		o.IDs = append(o.IDs, id)
	}
}

type envelope struct {
	Type   string `json:"type"`
	Output []struct {
		Type string `json:"type"`
	} `json:"output"`
	Item *struct {
		Type string `json:"type"`
	} `json:"item"`
	ID            string          `json:"id"`
	ResponseID    string          `json:"responseId"`
	Usage         json.RawMessage `json:"usage"`
	UsageMetadata json.RawMessage `json:"usageMetadata"`
	Response      *envelope       `json:"response"`
	Message       *envelope       `json:"message"`
	Conversation  json.RawMessage `json:"conversation"`
	Container     json.RawMessage `json:"container"`
}

func (o *Observer) observe(raw []byte) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "[DONE]" {
		return
	}
	var e envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		o.anomaly("cannot parse provider usage JSON")
		return
	}
	o.envelope(&e, 0)
}

func (o *Observer) envelope(e *envelope, depth int) {
	if depth > 2 {
		return
	}
	for _, kind := range []string{"web_search_call", "file_search_call", "code_interpreter_call", "image_generation_call", "mcp_call"} {
		if strings.HasPrefix(e.Type, "response."+kind+".") || e.Item != nil && e.Item.Type == kind {
			o.Usage.Uncovered = true
		}
		for _, item := range e.Output {
			if item.Type == kind {
				o.Usage.Uncovered = true
			}
		}
	}
	o.addID(e.ID)
	o.addID(e.ResponseID)
	for _, v := range []json.RawMessage{e.Conversation, e.Container} {
		if len(v) > 0 {
			var id string
			if json.Unmarshal(v, &id) != nil {
				var entry struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(v, &entry)
				id = entry.ID
			}
			o.addID(id)
		}
	}
	if e.Response != nil {
		o.envelope(e.Response, depth+1)
	}
	if e.Message != nil {
		o.envelope(e.Message, depth+1)
	}
	raw := e.Usage
	if o.Protocol == "gemini" {
		raw = e.UsageMetadata
	}
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		o.anomaly("invalid provider usage object")
		return
	}
	read := func(target **int64, path string) {
		parts := strings.Split(path, ".")
		m := fields
		for _, p := range parts[:len(parts)-1] {
			var next map[string]json.RawMessage
			if json.Unmarshal(m[p], &next) != nil {
				return
			}
			m = next
		}
		v, exists := m[parts[len(parts)-1]]
		if !exists || string(v) == "null" {
			return
		}
		var n int64
		if json.Unmarshal(v, &n) != nil || n < 0 {
			o.anomaly("invalid usage counter: " + path)
			return
		}
		*target = &n
	}
	for _, path := range []string{"prompt_tokens_details.audio_tokens", "completion_tokens_details.audio_tokens", "input_tokens_details.audio_tokens", "output_tokens_details.audio_tokens"} {
		var n *int64
		read(&n, path)
		if n != nil && *n > 0 {
			o.Usage.NonText = true
		}
	}
	switch o.Protocol {
	case "openai":
		read(&o.Usage.Input, "prompt_tokens")
		read(&o.Usage.Output, "completion_tokens")
		read(&o.Usage.Total, "total_tokens")
		read(&o.Usage.CacheRead, "prompt_tokens_details.cached_tokens")
		read(&o.Usage.Reasoning, "completion_tokens_details.reasoning_tokens")
	case "responses":
		read(&o.Usage.Input, "input_tokens")
		read(&o.Usage.Output, "output_tokens")
		read(&o.Usage.Total, "total_tokens")
		read(&o.Usage.CacheRead, "input_tokens_details.cached_tokens")
		read(&o.Usage.Reasoning, "output_tokens_details.reasoning_tokens")
	case "claude":
		read(&o.Usage.Input, "input_tokens")
		read(&o.Usage.Output, "output_tokens")
		read(&o.Usage.CacheRead, "cache_read_input_tokens")
		read(&o.Usage.CacheWrite, "cache_creation_input_tokens")
		read(&o.Usage.CacheWrite5m, "cache_creation.ephemeral_5m_input_tokens")
		read(&o.Usage.CacheWrite1h, "cache_creation.ephemeral_1h_input_tokens")
		var tools map[string]int64
		if json.Unmarshal(fields["server_tool_use"], &tools) == nil {
			for _, n := range tools {
				if n > 0 {
					o.Usage.Uncovered = true
				}
			}
		}
	case "gemini":
		read(&o.Usage.Input, "promptTokenCount")
		read(&o.Usage.Output, "candidatesTokenCount")
		read(&o.Usage.Total, "totalTokenCount")
		read(&o.Usage.CacheRead, "cachedContentTokenCount")
		read(&o.Usage.Reasoning, "thoughtsTokenCount")
		if v, ok := fields["toolUsePromptTokenCount"]; ok && string(v) != "0" {
			o.Usage.Uncovered = true
		}
		for _, name := range []string{"promptTokensDetails", "candidatesTokensDetails"} {
			var details []struct {
				Modality   string `json:"modality"`
				TokenCount int64  `json:"tokenCount"`
			}
			if json.Unmarshal(fields[name], &details) == nil {
				for _, d := range details {
					if d.TokenCount > 0 && (d.Modality == "AUDIO" || d.Modality == "VIDEO" || name == "candidatesTokensDetails" && d.Modality == "IMAGE") {
						o.Usage.NonText = true
					}
				}
			}
		}
	}
}
