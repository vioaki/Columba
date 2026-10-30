package billing

import (
	"columba.local/backend/internal/nativeusage"
	"encoding/json"
	"testing"
)

func TestCalculateProtocolAccounting(t *testing.T) {
	for _, tt := range []struct{ name, protocol, usage, prices, amount, status string }{
		{"openai inclusive cache and reasoning", "openai", `{"input":100,"output":20,"cacheRead":40,"reasoning":10}`, `{"input":2,"output":8,"cache_read":1}`, "0.000320000000", "complete"},
		{"separate reasoning rate", "responses", `{"input":100,"output":20,"reasoning":10}`, `{"input":2,"output":8,"reasoning":3}`, "0.000310000000", "complete"},
		{"gemini reasoning additional", "gemini", `{"input":100,"output":20,"cacheRead":40,"reasoning":10}`, `{"input":2,"output":8,"cache_read":1}`, "0.000400000000", "complete"},
		{"claude exclusive cache", "claude", `{"input":100,"output":20,"cacheRead":40,"cacheWrite":30,"cacheWrite5m":20,"cacheWrite1h":10}`, `{"input":2,"output":8,"cache_read":1,"cache_write":3,"cache_write_1h":4}`, "0.000500000000", "complete"},
		{"one hour not guessed", "claude", `{"input":100,"output":20,"cacheWrite":10,"cacheWrite1h":10}`, `{"input":2,"output":8,"cache_write":3}`, "0.000360000000", "partial"},
		{"zero known", "openai", `{"input":100,"output":20}`, `{"input":0,"output":0}`, "0.000000000000", "complete"},
		{"missing prices", "openai", `{"input":100,"output":20}`, `{}`, "", "missing_price"},
		{"missing usage", "openai", `{}`, `{"input":0,"output":0}`, "", "missing_usage"},
		{"partial usage", "openai", `{"input":100}`, `{"input":2,"output":8}`, "0.000200000000", "partial"},
		{"inclusive cache invalid", "responses", `{"input":2,"output":4,"cacheRead":3}`, `{"input":2,"output":8,"cache_read":1}`, "", "missing_usage"},
		{"tier equality", "openai", `{"input":200000,"output":1}`, `{"input":1,"output":1,"tiers":[{"input":2,"output":2,"tier":{"type":"context","size":200000}}]}`, "0.200001000000", "complete"},
		{"tier exceeded", "openai", `{"input":200001,"output":1}`, `{"input":1,"output":1,"tiers":[{"input":2,"output":2,"tier":{"type":"context","size":200000}}]}`, "0.400004000000", "complete"},
		{"claude tier includes cache", "claude", `{"input":199990,"output":1,"cacheRead":11}`, `{"input":1,"output":1,"cache_read":0,"tiers":[{"input":2,"output":2,"tier":{"type":"context","size":200000}}]}`, "0.399982000000", "complete"},
		{"tier unknown context", "openai", `{"output":1}`, `{"output":1,"tiers":[{"output":2,"tier":{"type":"context","size":200000}}]}`, "", "missing_usage"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var u nativeusage.Usage
			var p Prices
			if err := json.Unmarshal([]byte(tt.usage), &u); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.prices), &p); err != nil {
				t.Fatal(err)
			}
			r := Calculate(tt.protocol, u, p, nil)
			amount := ""
			if r.Amount != nil {
				amount = *r.Amount
			}
			if amount != tt.amount || r.Status != tt.status {
				t.Fatalf("got %s %s %v", amount, r.Status, r.Reasons)
			}
		})
	}
}
