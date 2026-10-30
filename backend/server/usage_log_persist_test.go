package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"columba.local/backend/config"
	"columba.local/backend/storage"
)

// newPersistTestServer 带 store + 可写 config 的 Server（免登录直调 handler）。
func newPersistTestServer(t *testing.T) (*Server, *config.Config, *storage.Store) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "persist.sqlite3"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &config.Config{}
	cfg.SetDatabasePath(filepath.Join(t.TempDir(), "persist-config.sqlite3"))
	return &Server{store: store, config: cfg}, cfg, store
}

func storedRecordJSON(t *testing.T, store *storage.Store, id string) string {
	t.Helper()
	payload, found, err := store.GetUsageRecordJSON(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("record %s not found (err=%v)", id, err)
	}
	return string(payload)
}

func TestBodyOnErrorOnlyDropsSuccessBodies(t *testing.T) {
	s, cfg, store := newPersistTestServer(t)
	on := true
	cfg.SetUsageLogConfig(config.UsageLogConfig{BodyOnErrorOnly: &on})

	success := newExternalizeRecord("onerr-success")
	success.StatusCode = 200
	success.IncomingBody = usageBody{Content: `{"big":"payload"}`}
	if err := s.saveUsageRecordToStore(success); err != nil {
		t.Fatalf("save: %v", err)
	}
	stored := storedRecordJSON(t, store, "onerr-success")
	if strings.Contains(stored, "big") {
		t.Fatalf("success body must be dropped under bodyOnErrorOnly, got %s", stored)
	}
	if success.IncomingBody.Content != "" {
		t.Fatal("in-memory record bodies must be cleared before marshal")
	}
}

func TestBodyOnErrorOnlyKeepsFailedBodiesAndWritesAssets(t *testing.T) {
	s, cfg, store := newPersistTestServer(t)
	on := true
	cfg.SetUsageLogConfig(config.UsageLogConfig{BodyOnErrorOnly: &on})

	failed := newExternalizeRecord("onerr-failed")
	failed.StatusCode = 400
	failed.Error = "boom"
	failed.ErrorKind = "invalid_request"
	payload := strings.Repeat("A", 600)
	failed.IncomingBody = failed.sanitizeBody([]byte(`{"url":"data:image/png;base64,` + payload + `"}`))
	if failed.assets.count() != 1 {
		t.Fatal("precondition: image must be registered at capture time")
	}
	if err := s.saveUsageRecordToStore(failed); err != nil {
		t.Fatalf("save: %v", err)
	}

	stored := storedRecordJSON(t, store, "onerr-failed")
	if !strings.Contains(stored, AssetPlaceholderPrefix) {
		t.Fatalf("failed record must keep body with placeholder, got %s", stored)
	}
	if !strings.Contains(stored, `"errorKind":"invalid_request"`) {
		t.Fatalf("errorKind must be persisted, got %s", stored)
	}
	// 资产已写盘（扁平内容寻址：文件在根目录，不带请求子目录）。
	item := failed.assets.items[0]
	path := filepath.Join(s.usageAssetsRoot(), item.Hash+"."+item.Ext)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("asset file must be written for kept body: %v", err)
	}
	refs := referencedAssetFiles(t, s.store)
	if !refs[item.Hash+"."+item.Ext] {
		t.Fatalf("asset ref must be recorded: refs=%v", refs)
	}
	if !containsWarning(failed, "externalized") {
		t.Fatalf("RequestWarnings must note externalization: %v", failed.RequestWarnings)
	}
}

func containsWarning(record *usageRecord, substr string) bool {
	for _, w := range record.RequestWarnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestPersistDisabledDropsRecord(t *testing.T) {
	s, cfg, _ := newPersistTestServer(t)
	off := false
	cfg.SetUsageLogConfig(config.UsageLogConfig{PersistEnabled: &off})

	called := false
	// recordUsage 在入口即返回，不触达 store。
	s.recordUsage(&usageRecord{RequestID: "dropped", StartedAt: time.Now(), EndedAt: time.Now()})
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, items, _ := s.store.QueryUsageLogs(context.Background(), storage.UsageQuery{Limit: 10}); len(items) == 0 {
			called = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !called {
		t.Fatal("persistEnabled=false must stop recording usage")
	}
}

// 管理端资产路由：鉴权由 admin 组中间件负责，这里覆盖文件名校验与正常下发。
// 准备一条记录 + 一个真实资产文件。
// 正常获取：请求拥有资产引用时可下发，Content-Type 按 ext。
// 穿越与非法名一律 400/404。
func TestAdminUsageAssetServesFileAndRejectsTraversal(t *testing.T) {
	s, cfg, store := newPersistTestServer(t)
	record := newExternalizeRecord("req_asset")
	payload := strings.Repeat("B", 600)
	record.sanitizeBody([]byte(`{"url":"data:image/png;base64,` + payload + `"}`))
	if err := s.saveUsageRecordToStore(record); err != nil {
		t.Fatalf("save: %v", err)
	}
	router := newTestEngine(t)
	s.setupAdminRoutes(router.Group("/api/admin"))
	fileName := record.assets.items[0].Hash + "." + record.assets.items[0].Ext
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usage/assets/req_asset/"+fileName, nil)
	w := httptest.NewRecorder()
	serveTestHTTP(router, w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("asset status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type=%s", ct)
	}
	for _, path := range []string{
		"/api/admin/usage/assets/req_asset/..%2F..%2Fetc%2Fpasswd",
		"/api/admin/usage/assets/req_asset/short.png",
		"/api/admin/usage/assets/req_a.b/0123456789abcdef.png",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		serveTestHTTP(router, w, req)
		if w.Code == http.StatusOK {
			t.Fatalf("path %s must not return 200", path)
		}
	}
	_ = cfg
	_ = store
}
