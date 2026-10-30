package storage

import (
	"context"
	"path/filepath"

	"testing"
	"time"
)

func TestOpenEnablesWALAndInitializesCurrentSchema(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "elysia #1.sqlite3"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal_mode query error = %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	// Check both the initial connection and a replacement from the pool.
	for _, maxIdle := range []int{1, 0} {
		store.db.SetMaxIdleConns(maxIdle)
		var timeout, foreignKeys, synchronous, tempStore, cacheSize int
		if err := store.db.QueryRow(`SELECT
			(SELECT timeout FROM pragma_busy_timeout),
			(SELECT foreign_keys FROM pragma_foreign_keys),
			(SELECT synchronous FROM pragma_synchronous),
			(SELECT temp_store FROM pragma_temp_store),
			(SELECT cache_size FROM pragma_cache_size)`).Scan(&timeout, &foreignKeys, &synchronous, &tempStore, &cacheSize); err != nil {
			t.Fatal(err)
		}
		if timeout != 5000 || foreignKeys != 1 || synchronous != 1 || tempStore != 2 || cacheSize != -65536 {
			t.Fatalf("connection settings (maxIdle=%d): %d %d %d %d %d", maxIdle, timeout, foreignKeys, synchronous, tempStore, cacheSize)
		}
	}

	if err := store.createSchema(context.Background()); err != nil {
		t.Fatalf("repeated schema initialization error = %v", err)
	}
}

func TestSourceModelsGroupsTokensAndUsage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "elysia.sqlite3"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	source := ModelSource{ID: "src", Name: "Source", BaseURL: "https://example.test/v1", APIKey: "secret", Platform: "custom:openai-chat-completions", Enabled: true, AutoFetchModels: false}
	if err := store.UpsertSource(ctx, source); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if err := store.ReplaceSourceModels(ctx, source, []Model{{ID: "model-a", Name: "Model A", Type: "llm", Available: true}}); err != nil {
		t.Fatalf("ReplaceSourceModels() error = %v", err)
	}
	models, err := store.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].Platform != source.Platform || models[0].APIKey != "secret" {
		t.Fatalf("models = %#v", models)
	}

	if err := store.UpsertAPIToken(ctx, APIToken{Name: "default", Token: "tok", Enabled: true}); err != nil {
		t.Fatalf("UpsertAPIToken() error = %v", err)
	}
	if token, ok, err := findAPIToken(store, ctx, "tok"); err != nil || !ok || token.Name != "default" {
		t.Fatalf("FindAPIToken() = %#v, %v, %v", token, ok, err)
	}

	started := time.Now().UTC().Add(-time.Minute)
	usage := UsageLogItem{RequestID: "req", StartedAt: started, KeyName: "default", GroupName: "Group", ModelName: "Model A", SourceID: "src-1", StatusCode: 200, InputTokens: 1, OutputTokens: 2, TotalTokens: 3}
	if err := store.SaveUsageRecordJSON(ctx, []byte(`{"requestId":"req"}`), usage, time.Now().UTC()); err != nil {
		t.Fatalf("SaveUsageRecordJSON() error = %v", err)
	}
	total, logs, err := store.QueryUsageLogs(ctx, UsageQuery{From: started.Add(-time.Second), To: time.Now().UTC(), Limit: 10})
	if err != nil {
		t.Fatalf("QueryUsageLogs() error = %v", err)
	}
	if total != 1 || len(logs) != 1 || logs[0].TotalTokens != 3 || logs[0].SourceID != "src-1" {
		t.Fatalf("total=%d logs=%#v", total, logs)
	}
	summary, err := store.UsageTotals(ctx, UsageQuery{})
	if err != nil {
		t.Fatalf("UsageTotals() error = %v", err)
	}
	if summary["totalTokens"].(int) != 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

// 回归 #10：空库时 UsageTotals 不应因 SUM(CASE...) 返回 NULL 而 Scan 失败。
func TestUsageTotalsEmptyDB(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "empty.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	summary, err := store.UsageTotals(context.Background(), UsageQuery{})
	if err != nil {
		t.Fatalf("UsageTotals on empty db should not error, got: %v", err)
	}
	if summary["requests"].(int) != 0 || summary["success"].(int) != 0 || summary["failed"].(int) != 0 {
		t.Fatalf("empty db totals should be zero, got: %#v", summary)
	}
}

// 回归 #1：同一 token 值不允许配置到两个不同 name 上。
func TestUpsertAPITokenRejectsDuplicateValue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "dup.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.UpsertAPIToken(ctx, APIToken{Name: "k1", Token: "sk-same-value", Enabled: true}); err != nil {
		t.Fatalf("first upsert should succeed: %v", err)
	}
	// 不同 name 用同一 token 值 → 应被拒。
	if err := store.UpsertAPIToken(ctx, APIToken{Name: "k2", Token: "sk-same-value", Enabled: true}); err == nil {
		t.Fatalf("duplicate token value across names should be rejected")
	}
	// 同名更新自身（token 不变）→ 应允许。
	if err := store.UpsertAPIToken(ctx, APIToken{Name: "k1", Token: "sk-same-value", Enabled: false}); err != nil {
		t.Fatalf("updating same token's own record should succeed: %v", err)
	}
	// 不同 name 用不同 token 值 → 应允许。
	if err := store.UpsertAPIToken(ctx, APIToken{Name: "k3", Token: "sk-other-value", Enabled: true}); err != nil {
		t.Fatalf("distinct token value should succeed: %v", err)
	}
}

// 回归：时间过滤/排序改用 started_ms 整型列。此前按 RFC3339Nano 字符串比较，
// 整秒时间戳（…T00:00:00Z）与带毫秒时间戳（…T00:00:00.123Z）因 '.' < 'Z'
// 的字典序，整秒边界的记录会被 from=整秒 的过滤漏掉。
func TestUsageQueryWholeSecondBoundary(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "usage-boundary.sqlite3"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	boundary := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	older := UsageLogItem{RequestID: "req-exact", StartedAt: boundary, StatusCode: 200, InputTokens: 1}
	newer := UsageLogItem{RequestID: "req-millis", StartedAt: boundary.Add(123 * time.Millisecond), StatusCode: 200, InputTokens: 2}
	for _, item := range []UsageLogItem{older, newer} {
		payload := []byte(`{"requestId":"` + item.RequestID + `"}`)
		if err := store.SaveUsageRecordJSON(ctx, payload, item, item.StartedAt.Add(time.Second)); err != nil {
			t.Fatalf("SaveUsageRecordJSON(%s) error = %v", item.RequestID, err)
		}
	}

	total, logs, err := store.QueryUsageLogs(ctx, UsageQuery{From: boundary, To: boundary.Add(time.Minute), Limit: 10})
	if err != nil {
		t.Fatalf("QueryUsageLogs() error = %v", err)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("both whole-second and millisecond records must match the boundary filter, total=%d logs=%#v", total, logs)
	}
	if logs[0].RequestID != "req-millis" || logs[1].RequestID != "req-exact" {
		t.Fatalf("records must sort newest-first, got %s then %s", logs[0].RequestID, logs[1].RequestID)
	}
}

// Malformed timestamps must not create an epoch bucket in daily statistics.
func TestUnparseableStartedAtExcludedFromRollupIncludedInAllTimeTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-ts.sqlite3")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.LogStore.db.Exec(`INSERT INTO usage_records(request_id, started_at, ended_at, record_json, status_code, total_tokens) VALUES('bad','not-a-time','not-a-time','{}', 200, 3)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer store.Close()

	var startedMs int64
	if err := store.LogStore.db.QueryRow(`SELECT started_ms FROM usage_records WHERE request_id='bad'`).Scan(&startedMs); err != nil {
		t.Fatalf("query: %v", err)
	}
	if startedMs != 0 {
		t.Fatalf("unparseable started_ms = %d, want 0", startedMs)
	}

	if err := store.RunRollupBackfill(context.Background()); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var rollupCnt int
	if err := store.LogStore.db.QueryRow(`SELECT COALESCE(SUM(cnt),0) FROM usage_rollup_hour`).Scan(&rollupCnt); err != nil {
		t.Fatalf("rollup count: %v", err)
	}
	if rollupCnt != 0 {
		t.Fatalf("orphan timestamps must stay out of rollup, got %d", rollupCnt)
	}

	totals, err := store.UsageTotals(context.Background(), UsageQuery{})
	if err != nil {
		t.Fatalf("UsageTotals: %v", err)
	}
	if totals["requests"].(int) != 1 {
		t.Fatalf("all-time totals must include orphan row, got %#v", totals)
	}

	daily, err := store.UsageDaily(context.Background(), UsageQuery{}, 0)
	if err != nil {
		t.Fatalf("UsageDaily: %v", err)
	}
	if len(daily) != 0 {
		t.Fatalf("daily must not invent a 1970 bucket for orphan timestamps, got %#v", daily)
	}

	if _, err := store.LogStore.db.Exec(`UPDATE usage_records SET key_hash = 'kh', source_id = 'src' WHERE request_id = 'bad'`); err != nil {
		t.Fatalf("tag orphan: %v", err)
	}
	validAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	if _, err := store.LogStore.db.Exec(`INSERT INTO usage_records(request_id, started_at, started_ms, ended_at, model_name, key_hash, source_id, status_code, total_tokens, record_json) VALUES('ok', ?, ?, ?, 'm', 'kh', 'src', 200, 4, '{}')`,
		validAt.Format(time.RFC3339Nano), validAt.UnixMilli(), validAt.Add(time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert valid: %v", err)
	}

	assertNo1970 := func(t *testing.T, label string, buckets []UsageDailyBucket) {
		t.Helper()
		if len(buckets) == 0 {
			t.Fatalf("%s: expected the valid 2026 row, got empty", label)
		}
		for _, b := range buckets {
			if b.Date < "2020-01-01" {
				t.Fatalf("%s produced epoch-like bucket %#v", label, buckets)
			}
		}
	}

	store.rollupReady.Store(false)
	rawDaily, err := store.UsageDaily(context.Background(), UsageQuery{}, 0)
	store.rollupReady.Store(true)
	if err != nil {
		t.Fatalf("daily raw: %v", err)
	}
	assertNo1970(t, "rollup not ready", rawDaily)

	bySource, err := store.UsageDaily(context.Background(), UsageQuery{SourceIDs: []string{"src"}}, 0)
	if err != nil {
		t.Fatalf("daily sourceId: %v", err)
	}
	assertNo1970(t, "sourceId", bySource)

	halfHour, err := store.UsageDaily(context.Background(), UsageQuery{}, 5*60+30)
	if err != nil {
		t.Fatalf("daily +05:30: %v", err)
	}
	assertNo1970(t, "+05:30", halfHour)

	modelDaily, err := store.UsageByModelDaily(context.Background(), UsageQuery{SourceIDs: []string{"src"}}, 0, 8)
	if err != nil {
		t.Fatalf("by-model-daily: %v", err)
	}
	for _, b := range modelDaily {
		if b.Date < "2020-01-01" {
			t.Fatalf("UsageByModelDaily epoch-like %#v", modelDaily)
		}
	}
}

func TestStoreCloseCancelsRollupBackfill(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "close-rollup.sqlite3"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store.StartRollupBackfill()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
