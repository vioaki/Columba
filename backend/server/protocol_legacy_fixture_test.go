package server

import (
	"columba.local/backend/config"
	"columba.local/backend/protocol"
	"columba.local/backend/relay"
	"columba.local/backend/storage"
	"github.com/bytedance/sonic"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newAgentIntegrationServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{}
	store, err := storage.Open(filepath.Join(dir, "agent.sqlite3"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	s := &Server{config: cfg, engine: newTestEngine(t), protocolTransport: relay.NewProtocolTransport(10 * time.Second), affinity: newAffinityCache(), store: store, skipOutboundValidation: true}
	t.Cleanup(s.closeCaches)
	activateDiscoveryPresets(t, s)
	return s
}

func mustProtocolValue(t *testing.T, raw string) protocol.Value {
	t.Helper()
	value, err := protocol.ParseValue([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustEncodedProtocolValue(t *testing.T, value any) protocol.Value {
	t.Helper()
	encoded, err := protocol.EncodeValue(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// This protocol uses independent envelope mappings, with no built-in shape.
func agentEnvelopeDefinition(t *testing.T) protocol.Definition {
	t.Helper()
	request := mustProtocolValue(t, `{"schemaVersion":1,"model":"m","content":[{"kind":"message","role":"user","children":[{"kind":"text","payload":"hello"}]},{"kind":"tool_call","name":"lookup","callId":"c","input":{"kind":"json","value":{"x":1}}},{"kind":"tool_result","callId":"c","payload":"ok"}],"tools":[{"kind":"function","name":"lookup","inputSchema":{"type":"object"}}]}`)
	response := mustProtocolValue(t, `{"schemaVersion":1,"id":"r","content":[{"kind":"text","payload":"hi"},{"kind":"tool_call","name":"lookup","callId":"c2","input":{"kind":"json","value":{"x":2}}}]}`)
	definition := protocol.Definition{SchemaVersion: 2, ID: "agent-envelope", Name: "agent-envelope", Version: "1", Family: "agent-envelope", WireVersion: "1", Capabilities: protocol.CapabilitySet{protocol.TextCapability: true, protocol.FunctionToolsCapability: true}, Directions: map[protocol.Direction]protocol.Mapping{}, Operations: map[string]protocol.Operation{"generate": {Kind: "generate", Method: "POST", Path: "/generate", Transport: protocol.HTTPJSON, Auth: protocol.Credential{Location: "none"}}}}
	for _, pair := range []struct {
		decode, encode protocol.Direction
		value          protocol.Value
		fields         []string
	}{
		{protocol.DecodeRequest, protocol.EncodeRequest, request, []string{"model", "content", "tools", "parameters", "toolChoice"}},
		{protocol.DecodeResponse, protocol.EncodeResponse, response, []string{"id", "content", "status", "usage"}},
	} {
		fields := map[string]protocol.Expression{"schemaVersion": {Op: "literal", Value: mustProtocolValue(t, "1")}}
		object, _ := pair.value.ReadObject()
		for _, field := range pair.fields {
			fields[field] = protocol.Expression{Op: "read", From: "input", Path: "/" + field}
		}
		wire := mustEncodedProtocolValue(t, map[string]any{"payload": object})
		definition.Directions[pair.decode] = protocol.Mapping{Transform: &protocol.Expression{Op: "read", From: "input", Path: "/payload"}}
		definition.Directions[pair.encode] = protocol.Mapping{Transform: &protocol.Expression{Op: "object", Fields: map[string]protocol.Expression{"payload": {Op: "object", Fields: fields}}}}
		definition.Samples = append(definition.Samples, protocol.Sample{ID: string(pair.decode), Direction: pair.decode, Input: wire, Expected: pair.value}, protocol.Sample{ID: string(pair.encode), Direction: pair.encode, Input: pair.value, Expected: wire})
	}
	definition.Agent = &protocol.AgentConfig{
		ToolResults: protocol.JSONInput,
		Parameters:  protocol.Mapping{Transform: &protocol.Expression{Op: "object", Fields: map[string]protocol.Expression{"max_output_tokens": {Op: "read", From: "input", Path: "/maxOutputTokens"}}}},
		Samples:     []protocol.AgentSample{{ID: "agent-disabled", Preferences: protocol.AgentPreferences{MaxOutputTokens: 8192}, Expected: mustProtocolValue(t, `{"max_output_tokens":8192}`)}},
	}
	return definition
}
func newOpsTestServer(t *testing.T) *Server { return newAgentIntegrationServer(t) }

type capturedUpstreamRequest struct {
	Method string
	Path   string
	Auth   string // Authorization 头
	APIKey string // x-api-key 头
	Body   string
}

type capturingUpstream struct {
	*httptest.Server
	mu      sync.Mutex
	calls   []capturedUpstreamRequest
	handler func(w http.ResponseWriter, body string, call int)
}

func newCapturingUpstream(t *testing.T, handler func(w http.ResponseWriter, body string, call int)) *capturingUpstream {
	t.Helper()
	upstream := &capturingUpstream{handler: handler}
	upstream.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstream.mu.Lock()
		upstream.calls = append(upstream.calls, capturedUpstreamRequest{
			Method: r.Method, Path: r.URL.Path,
			Auth: r.Header.Get("Authorization"), APIKey: r.Header.Get("x-api-key"),
			Body: string(body),
		})
		call := len(upstream.calls)
		upstream.mu.Unlock()
		if handler != nil {
			handler(w, string(body), call)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func (u *capturingUpstream) requestCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.calls)
}

func (u *capturingUpstream) last() capturedUpstreamRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.calls) == 0 {
		return capturedUpstreamRequest{}
	}
	return u.calls[len(u.calls)-1]
}

func openAIChunk(id string, delta map[string]any, finish string, usage map[string]any) string {
	choice := map[string]any{"index": 0, "delta": delta}
	if finish != "" {
		choice["finish_reason"] = finish
	}
	chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": 1, "model": "fake-model", "choices": []any{choice}}
	if usage != nil {
		chunk["usage"] = usage
	}
	encoded, _ := sonic.ConfigStd.Marshal(chunk)
	return "data: " + string(encoded) + "\n\n"
}

func openAIDone() string { return "data: [DONE]\n\n" }

func toolCallDelta(index int, id, name, args string) map[string]any {
	function := map[string]any{"arguments": args}
	if name != "" {
		function["name"] = name
	}
	tool := map[string]any{"index": index, "function": function}
	if id != "" {
		tool["id"] = id
		tool["type"] = "function"
	}
	return map[string]any{"tool_calls": []any{tool}}
}
