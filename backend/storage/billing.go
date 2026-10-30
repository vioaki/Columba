package storage

import (
	"columba.local/backend/internal/billing"
	"columba.local/backend/internal/nativeusage"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"
)

const billingSchema = `CREATE TABLE IF NOT EXISTS billing_attempts (
 request_id TEXT NOT NULL, attempt INTEGER NOT NULL, started_ms INTEGER NOT NULL,
 model_id TEXT NOT NULL, source_id TEXT NOT NULL, source_name TEXT NOT NULL DEFAULT '',
 key_name TEXT NOT NULL, key_hash TEXT NOT NULL, protocol TEXT NOT NULL,
 http_status INTEGER NOT NULL DEFAULT 0, usage_json TEXT NOT NULL DEFAULT '{}',
 price_json TEXT NOT NULL DEFAULT '{}', amount TEXT, completeness TEXT NOT NULL,
 reasons_json TEXT NOT NULL DEFAULT '[]', PRIMARY KEY(request_id,attempt));
 CREATE INDEX IF NOT EXISTS idx_billing_time ON billing_attempts(started_ms);`

type BillingAttempt struct {
	RequestID  string            `json:"requestId"`
	Attempt    int               `json:"attempt"`
	StartedMs  int64             `json:"startedMs"`
	ModelID    string            `json:"modelId"`
	SourceID   string            `json:"sourceId"`
	SourceName string            `json:"sourceName"`
	KeyName    string            `json:"keyName"`
	KeyHash    string            `json:"-"`
	Protocol   string            `json:"protocol"`
	HTTPStatus int               `json:"httpStatus"`
	Usage      nativeusage.Usage `json:"usage"`
	Price      json.RawMessage   `json:"price"`
	billing.Result
}

func (s *LogStore) StartBillingAttempt(ctx context.Context, a BillingAttempt) error {
	if a.RequestID == "" || a.Attempt < 1 || !json.Valid(a.Price) {
		return fmt.Errorf("invalid billing attempt")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO billing_attempts(request_id,attempt,started_ms,model_id,source_id,source_name,key_name,key_hash,protocol,price_json,completeness) VALUES(?,?,?,?,?,?,?,?,?,?,'pending') ON CONFLICT(request_id,attempt) DO NOTHING`, a.RequestID, a.Attempt, a.StartedMs, a.ModelID, a.SourceID, a.SourceName, a.KeyName, a.KeyHash, a.Protocol, string(a.Price))
	return err
}
func (s *LogStore) FinishBillingAttempt(ctx context.Context, a BillingAttempt) error {
	usage, err := json.Marshal(a.Usage)
	if err != nil {
		return err
	}
	reasons, err := json.Marshal(a.Reasons)
	if err != nil {
		return err
	}
	if a.Amount != nil {
		if r, ok := new(big.Rat).SetString(*a.Amount); !ok || r.Sign() < 0 {
			return fmt.Errorf("invalid billing amount")
		}
	}
	switch a.Status {
	case "complete", "partial", "missing_price", "missing_usage":
	default:
		return fmt.Errorf("invalid billing status")
	}
	_, err = s.db.ExecContext(ctx, `UPDATE billing_attempts SET http_status=?,usage_json=?,amount=?,completeness=?,reasons_json=? WHERE request_id=? AND attempt=? AND completeness='pending'`, a.HTTPStatus, string(usage), a.Amount, a.Status, string(reasons), a.RequestID, a.Attempt)
	return err
}

type BillingFilter struct {
	From, To                    int64
	Model, Source, Key, Request string
	Offset                      int
}

func (f BillingFilter) query() (string, []any) {
	q := " WHERE 1=1"
	args := []any{}
	if f.From > 0 {
		q += " AND started_ms>=?"
		args = append(args, f.From)
	}
	if f.To > 0 {
		q += " AND started_ms<?"
		args = append(args, f.To)
	}
	for _, v := range []struct{ column, value string }{{"model_id", f.Model}, {"source_id", f.Source}, {"key_name", f.Key}, {"request_id", f.Request}} {
		if v.value != "" {
			q += " AND " + v.column + "=?"
			args = append(args, v.value)
		}
	}
	return q, args
}

type BillingTotal struct {
	Name     string `json:"name"`
	Amount   string `json:"amount"`
	Attempts int    `json:"attempts"`
	Unpriced int    `json:"unpriced"`
	sum      *big.Rat
}

func (t *BillingTotal) add(amount *string, status string) error {
	t.Attempts++
	if status != "complete" {
		t.Unpriced++
	}
	if t.sum == nil {
		t.sum = new(big.Rat)
	}
	if amount != nil {
		v, ok := new(big.Rat).SetString(*amount)
		if !ok {
			return fmt.Errorf("invalid stored amount")
		}
		t.sum.Add(t.sum, v)
	}
	t.Amount = t.sum.FloatString(12)
	return nil
}

type BillingSummary struct {
	BillingTotal
	Requests int            `json:"requests"`
	Models   []BillingTotal `json:"models"`
	Sources  []BillingTotal `json:"sources"`
	Keys     []BillingTotal `json:"keys"`
	Trend    []BillingTotal `json:"trend"`
}

func (s *LogStore) BillingSummary(ctx context.Context, f BillingFilter) (BillingSummary, error) {
	result := BillingSummary{BillingTotal: BillingTotal{Amount: "0.000000000000"}}
	where, args := f.query()
	rows, err := s.readDB.QueryContext(ctx, `SELECT model_id,source_id,key_name,started_ms,amount,completeness FROM billing_attempts`+where, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	groups := []map[string]*BillingTotal{{}, {}, {}, {}}
	for rows.Next() {
		var model, source, key, status string
		var at int64
		var amount *string
		if err := rows.Scan(&model, &source, &key, &at, &amount, &status); err != nil {
			return result, err
		}
		if err := result.add(amount, status); err != nil {
			return result, err
		}
		for i, name := range []string{model, source, key, time.UnixMilli(at).UTC().Format("2006-01-02")} {
			if groups[i][name] == nil {
				groups[i][name] = &BillingTotal{Name: name}
			}
			if err := groups[i][name].add(amount, status); err != nil {
				return result, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	if err := s.readDB.QueryRowContext(ctx, `SELECT count(DISTINCT request_id) FROM billing_attempts`+where, args...).Scan(&result.Requests); err != nil {
		return result, err
	}
	lists := []*[]BillingTotal{&result.Models, &result.Sources, &result.Keys, &result.Trend}
	for i, g := range groups {
		*lists[i] = []BillingTotal{}
		for _, v := range g {
			*lists[i] = append(*lists[i], *v)
		}
		sort.Slice(*lists[i], func(a, b int) bool { return (*lists[i])[a].Name < (*lists[i])[b].Name })
	}
	return result, nil
}
func (s *LogStore) BillingAttempts(ctx context.Context, f BillingFilter) ([]BillingAttempt, error) {
	where, args := f.query()
	args = append(args, 100, max(0, f.Offset))
	rows, err := s.readDB.QueryContext(ctx, `SELECT request_id,attempt,started_ms,model_id,source_id,source_name,key_name,protocol,http_status,usage_json,price_json,amount,completeness,reasons_json FROM billing_attempts`+where+` ORDER BY started_ms DESC,request_id,attempt LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BillingAttempt{}
	for rows.Next() {
		var a BillingAttempt
		var usage, price, reasons string
		if err := rows.Scan(&a.RequestID, &a.Attempt, &a.StartedMs, &a.ModelID, &a.SourceID, &a.SourceName, &a.KeyName, &a.Protocol, &a.HTTPStatus, &usage, &price, &a.Amount, &a.Status, &reasons); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(usage), &a.Usage); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(reasons), &a.Reasons); err != nil {
			return nil, err
		}
		a.Price = json.RawMessage(price)
		items = append(items, a)
	}
	return items, rows.Err()
}
