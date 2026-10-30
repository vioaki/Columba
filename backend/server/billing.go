package server

import (
	"columba.local/backend/internal/billing"
	"columba.local/backend/storage"
	"context"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app"
	"strconv"
	"time"
)

func (s *Server) finishNativeBilling(record *usageRecord, attempt nativeAttemptRecord, snapshot priceSnapshot) {
	result := billing.Calculate(attempt.Protocol, attempt.Usage, snapshot.Prices, attempt.Anomalies)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.store.FinishBillingAttempt(ctx, storage.BillingAttempt{RequestID: record.RequestID, Attempt: attempt.Attempt, HTTPStatus: attempt.Status, Usage: attempt.Usage, Result: result})
	if err != nil {
		record.RequestWarnings = append(record.RequestWarnings, "billing settlement failed; pending entry retained")
		s.logEvent("error").Err(err).Str("request_id", record.RequestID).Int("attempt", attempt.Attempt).Msg("billing settlement failed")
	}
}
func billingFilter(c *app.RequestContext) (storage.BillingFilter, error) {
	f := storage.BillingFilter{Model: c.Query("model"), Source: c.Query("source"), Key: c.Query("key"), Request: c.Query("request")}
	for _, field := range []struct {
		name   string
		target *int64
	}{{"from", &f.From}, {"to", &f.To}} {
		if raw := c.Query(field.name); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v < 0 {
				return f, fmt.Errorf("invalid %s timestamp", field.name)
			}
			*field.target = v
		}
	}
	if f.From > 0 && f.To > 0 && f.To <= f.From {
		return f, fmt.Errorf("to must be after from")
	}
	if raw := c.Query("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return f, fmt.Errorf("invalid offset")
		}
		f.Offset = v
	}
	return f, nil
}
func (s *Server) adminBilling(ctx context.Context, c *app.RequestContext) {
	f, err := billingFilter(c)
	if err != nil {
		respondFail(ctx, c, 400, "invalid_filter", err.Error())
		return
	}
	result, err := s.store.BillingSummary(ctx, f)
	if err != nil {
		respondFail(ctx, c, 500, "billing_failed", err.Error())
		return
	}
	respondOK(ctx, c, result)
}
func (s *Server) adminBillingAttempts(ctx context.Context, c *app.RequestContext) {
	f, err := billingFilter(c)
	if err != nil {
		respondFail(ctx, c, 400, "invalid_filter", err.Error())
		return
	}
	result, err := s.store.BillingAttempts(ctx, f)
	if err != nil {
		respondFail(ctx, c, 500, "billing_failed", err.Error())
		return
	}
	respondOK(ctx, c, map[string]any{"items": result})
}
