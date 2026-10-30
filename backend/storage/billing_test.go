package storage

import (
	"columba.local/backend/internal/billing"
	"columba.local/backend/internal/nativeusage"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestBillingIsIdempotentExactAndIndependentOfLogCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.sqlite3")
	s, err := OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	amount := "0.000000000001"
	input := int64(1)
	for i := 1; i <= 3; i++ {
		a := BillingAttempt{RequestID: "request", Attempt: i, StartedMs: 1, ModelID: "m", SourceID: "s", KeyName: "key", Protocol: "openai", Price: json.RawMessage(`{"prices":{"input":"1"}}`), Usage: nativeusage.Usage{Input: &input}, Result: billing.Result{Amount: &amount, Status: "complete"}}
		if err := s.StartBillingAttempt(t.Context(), a); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishBillingAttempt(t.Context(), a); err != nil {
			t.Fatal(err)
		}
		changed := "1000"
		a.Amount = &changed
		a.Price = json.RawMessage(`{"prices":{"input":"999"}}`)
		if err := s.StartBillingAttempt(t.Context(), a); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishBillingAttempt(t.Context(), a); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.StartBillingAttempt(t.Context(), BillingAttempt{RequestID: "interrupted", Attempt: 1, StartedMs: 2, Price: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearUsage(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenLogStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := s.BillingSummary(t.Context(), BillingFilter{})
	if err != nil || summary.Amount != "0.000000000003" || summary.Requests != 2 || summary.Attempts != 4 || summary.Unpriced != 1 {
		t.Fatal(summary, err)
	}
	entries, err := s.BillingAttempts(t.Context(), BillingFilter{Request: "request"})
	if err != nil || len(entries) != 3 || string(entries[0].Price) != `{"prices":{"input":"1"}}` || entries[0].Usage.Input == nil || *entries[0].Usage.Input != 1 {
		t.Fatal(entries, err)
	}
	filtered, err := s.BillingSummary(t.Context(), BillingFilter{Key: "key", From: 1, To: 2})
	if err != nil || filtered.Attempts != 3 || filtered.Unpriced != 0 {
		t.Fatal(filtered, err)
	}
}
func TestUpgradeKeepsHistoricalUsageUnpriced(t *testing.T) {
	path, logs, key := previousDatabases(t)
	db, err := openUpgradeDatabase(logs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_records(request_id,started_at,ended_at,record_json,input_tokens,output_tokens,usage_report_mask) VALUES('historical','','','{"usage":{"inputTokens":12,"outputTokens":3}}',12,3,1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := UpgradeDatabases(path, logs, ""); err != nil {
		t.Fatal(err)
	}
	s, err := OpenWithKey(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries, err := s.BillingAttempts(t.Context(), BillingFilter{})
	if err != nil || len(entries) != 1 || entries[0].Status != "historical_unpriced" || entries[0].Amount != nil {
		t.Fatal(entries, err)
	}
	if err := s.ClearUsage(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err = s.BillingAttempts(t.Context(), BillingFilter{})
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
}
