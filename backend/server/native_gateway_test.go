package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"columba.local/backend/config"
	"columba.local/backend/internal/billing"
	"columba.local/backend/storage"
)

func newNativeTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, config: &config.Config{HTTPTimeout: 5}, engine: newTestEngine(t), skipOutboundValidation: true}
	t.Cleanup(func() { s.stopUsageWriter(); s.nativeHTTPClient().CloseIdleConnections(); store.Close() })
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/messages/count_tokens", "/v1beta/models/*action"} {
		s.engine.POST(path, s.serveNative)
	}
	return s
}

func seedNative(t *testing.T, s *Server, id, wire, base string) {
	t.Helper()
	source := storage.ModelSource{ID: id, Name: id, BaseURL: base, Platform: wire, APIKey: "upstream-secret", Enabled: true}
	if err := s.store.UpsertSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReplaceSourceModels(t.Context(), source, []storage.Model{{ID: "raw-model", Name: "Display Name", Platform: wire, Enabled: true, Available: true}}); err != nil {
		t.Fatal(err)
	}
	p := storage.DefaultModelRouting("raw-model", wire)
	p.RetryInterval = 0
	if err := s.store.SaveModelRouting(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}

func TestNativePassesExactBodiesAndEventsForAllProtocols(t *testing.T) {
	for _, tc := range []struct{ wire, path, plain, stream, auth, value string }{
		{"openai", "/v1/chat/completions", `{"choices":[{"message":{"tool_calls":[{"new_field":true}]}}],"unknown":9007199254740993,"usage":{"prompt_tokens":10,"completion_tokens":2}}`, "event: unknown\ndata: {\"vendor\":true}\n\ndata: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n", "Authorization", "Bearer upstream-secret"},
		{"responses", "/v1/responses", `{"id":"resp_x","output":[],"unknown":true,"usage":{"input_tokens":10,"output_tokens":2}}`, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_x\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n", "Authorization", "Bearer upstream-secret"},
		{"claude", "/v1/messages", `{"id":"msg_x","content":[{"type":"tool_use","custom":"x"}],"usage":{"input_tokens":10,"output_tokens":2}}`, "event: message_start\ndata: {\"message\":{\"id\":\"msg_x\",\"usage\":{\"input_tokens\":10}}}\n\nevent: message_delta\ndata: {\"usage\":{\"output_tokens\":2}}\n\n", "x-api-key", "upstream-secret"},
		{"gemini", "/v1beta/models/raw-model:generateContent", `{"candidates":[],"extra":"unchanged","usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}`, "data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2}}\n\n", "x-goog-api-key", "upstream-secret"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.wire+map[bool]string{true: "-stream"}[stream], func(t *testing.T) {
				requestBody := `{ "model" : "raw-model", "stream": ` + map[bool]string{true: "true", false: "false"}[stream] + `, "vendor_extension": {"n":9007199254740993}, "tools": [{"type":"function","unknown":true}] }`
				responseBody := tc.plain
				if stream {
					responseBody = tc.stream
				}
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					got, _ := io.ReadAll(r.Body)
					if string(got) != requestBody {
						t.Error("request body changed", string(got))
					}
					if r.Header.Get(tc.auth) != tc.value {
						t.Error("upstream credential missing")
					}
					if r.Header.Get("X-Vendor-Feature") != "preserve" {
						t.Error("unknown header removed")
					}
					if r.URL.Query().Get("key") != "" {
						t.Error("client key leaked")
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					w.Header().Set("X-Upstream", "preserved")
					io.WriteString(w, responseBody)
				}))
				defer provider.Close()
				s := newNativeTestServer(t)
				seedNative(t, s, "one", tc.wire, provider.URL)
				gateway := startTestServer(s.engine)
				defer gateway.Close()
				request, _ := http.NewRequest(http.MethodPost, gateway.URL+tc.path+"?key=client-secret", strings.NewReader(requestBody))
				request.Header.Set("X-Vendor-Feature", "preserve")
				response, err := gateway.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != 200 || string(got) != responseBody || response.Header.Get("X-Upstream") != "preserved" {
					t.Fatalf("raw response changed: %d %q %v", response.StatusCode, got, err)
				}
			})
		}
	}
}

func TestNativeRetryRecordsFailedUsageAndStopsAfterOutput(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			io.WriteString(w, `{"usage":{"prompt_tokens":7},"error":"retry"}`)
			return
		}
		io.WriteString(w, `{"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	}))
	defer provider.Close()
	s := newNativeTestServer(t)
	seedNative(t, s, "a", "openai", provider.URL)
	seedNative(t, s, "b", "openai", provider.URL)
	for _, id := range []string{"a", "b"} {
		price := billing.Rate("1")
		if err := s.store.SaveModelPrice(t.Context(), storage.ModelPrice{SourceID: id, ModelID: "raw-model", Override: billing.Prices{Rates: billing.Rates{Input: &price, Output: &price}}}); err != nil {
			t.Fatal(err)
		}
	}
	c, rec := adminTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"raw-model"}`)
	s.serveNative(contextForTest(c), c)
	flushTestResponse(c)
	if calls.Load() != 2 || rec.Code != 200 {
		t.Fatal(calls.Load(), rec.Code, rec.Body)
	}
	logs, err := s.store.UsageTotals(t.Context(), storage.UsageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if logs["inputTokens"] != 10 || logs["outputTokens"] != 2 {
		t.Fatal("failed attempt usage lost", logs)
	}
	summary, err := s.store.BillingSummary(t.Context(), storage.BillingFilter{})
	if err != nil || summary.Amount != "0.000012000000" || summary.Attempts != 2 || summary.Requests != 1 || summary.Unpriced != 1 {
		t.Fatal(summary, err)
	}
	var second atomic.Int32
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"partial\":true}\n\n")
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer broken.Close()
	s2 := newNativeTestServer(t)
	seedNative(t, s2, "a", "openai", broken.URL)
	seedNative(t, s2, "b", "openai", provider.URL)
	gateway := startTestServer(s2.engine)
	defer gateway.Close()
	response, err := gateway.Client().Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"raw-model","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if second.Load() != 1 || calls.Load() != 2 || !strings.Contains(string(body), "partial") {
		t.Fatal("retried after output", string(body), calls.Load())
	}
}

func TestNativeContinuationBindsOriginalChannelAndCredential(t *testing.T) {
	var a, b atomic.Int32
	upstream := func(calls *atomic.Int32, id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			io.WriteString(w, `{"id":"`+id+`","output":[]}`)
		}))
	}
	pa, pb := upstream(&a, "resp_a"), upstream(&b, "resp_b")
	defer pa.Close()
	defer pb.Close()
	s := newNativeTestServer(t)
	seedNative(t, s, "a", "responses", pa.URL)
	seedNative(t, s, "b", "responses", pb.URL)
	call := func(body string) int {
		c, r := adminTestContext(http.MethodPost, "/v1/responses", body)
		s.serveNative(contextForTest(c), c)
		flushTestResponse(c)
		return r.Code
	}
	if call(`{"model":"raw-model"}`) != 200 || call(`{"model":"raw-model","previous_response_id":"resp_a"}`) != 200 || a.Load() != 2 || b.Load() != 0 {
		t.Fatal(a.Load(), b.Load())
	}
	if call(`{"model":"raw-model","previous_response_id":"unknown"}`) != 409 {
		t.Fatal("unknown state was rerouted")
	}
	s.store.UpdateSourceEnabled(t.Context(), "a", false)
	if call(`{"model":"raw-model","previous_response_id":"resp_a"}`) != 409 || b.Load() != 0 {
		t.Fatal("disabled continuation moved to another channel")
	}
}

func TestNativeCancellationDoesNotRetry(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		calls.Add(1)
		close(started)
		<-r.Context().Done()
	}))
	defer provider.Close()
	s := newNativeTestServer(t)
	seedNative(t, s, "a", "openai", provider.URL)
	seedNative(t, s, "b", "openai", provider.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c, _ := adminTestContext(http.MethodPost, "/v1/chat/completions", `{"model":"raw-model"}`)
		s.serveNative(ctx, c)
		close(done)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not stop")
	}
	if calls.Load() != 1 {
		t.Fatal("canceled request retried")
	}
}
