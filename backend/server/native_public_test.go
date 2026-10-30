package server

import (
	"columba.local/backend/storage"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativePublicModelsAuthAndLiveEdits(t *testing.T) {
	s, _ := newAdminTestServer(t)
	s.setupRoutes()
	for _, source := range []struct{ id, wire string }{{"a", "openai"}, {"b", "openai"}, {"c", "claude"}, {"d", "gemini"}} {
		seedNative(t, s, source.id, source.wire, "https://example.com")
	}
	if err := s.store.UpsertAPIToken(t.Context(), storage.APIToken{Name: "local", Token: "secret", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	call := func(path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		rec := httptest.NewRecorder()
		serveTestHTTP(s.engine, rec, req)
		return rec
	}
	if r := call("/v1/models", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	r := call("/v1/models", "secret")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Data) != 1 || result.Data[0].ID != "raw-model" {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := call("/v1beta/models?key=secret", ""); r.Code != 200 || !strings.Contains(r.Body.String(), "models/raw-model") {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if _, err := s.store.UpdateSourceEnabled(t.Context(), id, false); err != nil {
			t.Fatal(err)
		}
	}
	r = call("/v1/models", "secret")
	if json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Data) != 0 {
		t.Fatal(r.Body.String())
	}
	if _, err := s.store.UpdateSourceEnabled(t.Context(), "a", true); err != nil {
		t.Fatal(err)
	}
	r = call("/v1/models", "secret")
	if json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Data) != 1 {
		t.Fatal(r.Body.String())
	}
	if err := s.store.DeleteSource(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	r = call("/v1/models", "secret")
	if json.Unmarshal(r.Body.Bytes(), &result) != nil || len(result.Data) != 0 {
		t.Fatal(r.Body.String())
	}
	if err := s.store.DeleteAPIToken(t.Context(), "local"); err != nil {
		t.Fatal(err)
	}
	if r := call("/v1/models", "secret"); r.Code != http.StatusUnauthorized {
		t.Fatal(r.Code)
	}
}
