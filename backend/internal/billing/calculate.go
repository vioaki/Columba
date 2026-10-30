package billing

import (
	"columba.local/backend/internal/nativeusage"
	"math/big"
)

type Result struct {
	Amount  *string  `json:"amount"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

// Calculate normalizes only counters. The wire request/response is never rewritten.
// Amounts are rounded once per attempt to 12 USD decimal places, then summed exactly.
func Calculate(protocol string, u nativeusage.Usage, p Prices, anomalies []string) Result {
	result := Result{Status: "complete", Reasons: append([]string(nil), anomalies...)}
	if err := p.Validate(); err != nil {
		return Result{Status: "missing_price", Reasons: []string{err.Error()}}
	}
	anyUsage := false
	for _, n := range []*int64{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.CacheWrite5m, u.CacheWrite1h, u.Reasoning} {
		if n != nil {
			anyUsage = true
			if *n < 0 {
				return Result{Status: "missing_usage", Reasons: []string{"negative usage counter"}}
			}
		}
	}
	if !anyUsage {
		return Result{Status: "missing_usage", Reasons: append(result.Reasons, "provider did not report usage")}
	}
	if u.NonText {
		return Result{Status: "partial", Reasons: append(result.Reasons, "multimodal token usage requires prices outside text/cache pricing; amount unavailable")}
	}
	n := func(v *int64) int64 {
		if v == nil {
			return 0
		}
		return *v
	}
	input, output, read, write, reason := n(u.Input), n(u.Output), n(u.CacheRead), n(u.CacheWrite), n(u.Reasoning)
	contextTokens := big.NewInt(input)
	if protocol == "claude" {
		contextTokens.Add(contextTokens, big.NewInt(read))
		if u.CacheWrite != nil {
			contextTokens.Add(contextTokens, big.NewInt(write))
		} else {
			contextTokens.Add(contextTokens, big.NewInt(n(u.CacheWrite5m)))
			contextTokens.Add(contextTokens, big.NewInt(n(u.CacheWrite1h)))
		}
	}
	rates := p.Rates
	if len(p.Tiers) > 0 && u.Input == nil {
		return Result{Status: "missing_usage", Reasons: []string{"input context is required to select a price tier"}}
	}
	for _, tier := range p.Tiers {
		if contextTokens.Cmp(big.NewInt(tier.Tier.Size)) > 0 {
			rates = rates.Overlay(tier.Rates)
		}
	}
	total := new(big.Rat)
	priced := false
	missing := false
	charge := func(count int64, rate *Rate, label string) {
		// Zero usage of an optional component needs no rate; required input/output are checked below.
		if count == 0 {
			return
		}
		if rate == nil {
			missing = true
			result.Reasons = append(result.Reasons, "missing "+label+" price")
			return
		}
		r, err := rate.Rat()
		if err != nil {
			missing = true
			result.Reasons = append(result.Reasons, err.Error())
			return
		}
		priced = true
		cost := new(big.Rat).Mul(r, new(big.Rat).SetInt64(count))
		cost.Quo(cost, big.NewRat(1000000, 1))
		total.Add(total, cost)
	}
	required := func(count *int64, rate *Rate, label string) {
		if count == nil {
			missing = true
			result.Reasons = append(result.Reasons, "missing "+label+" usage")
		} else if *count == 0 {
			priced = true
		}
	}
	required(u.Input, rates.Input, "input")
	required(u.Output, rates.Output, "output")
	invalid := func(message string) Result {
		return Result{Status: "missing_usage", Reasons: append(result.Reasons, message)}
	}
	switch protocol {
	case "openai", "responses", "gemini":
		if u.Input != nil && read > input {
			return invalid("cached tokens exceed inclusive input tokens")
		}
		if u.Input != nil {
			input -= read
		}
		if protocol != "gemini" && rates.Reasoning != nil {
			if u.Output != nil && reason > output {
				return invalid("reasoning tokens exceed inclusive output tokens")
			}
			if u.Output != nil {
				output -= reason
			}
			charge(reason, rates.Reasoning, "reasoning")
		}
		if protocol == "gemini" {
			rate := rates.Reasoning
			if rate == nil {
				rate = rates.Output
			}
			charge(reason, rate, "reasoning")
		}
		charge(read, rates.CacheRead, "cache read")
	case "claude":
		charge(read, rates.CacheRead, "cache read")
		if u.CacheWrite5m != nil || u.CacheWrite1h != nil {
			short, long := n(u.CacheWrite5m), n(u.CacheWrite1h)
			if u.CacheWrite != nil && (short > write || long > write-short) {
				return invalid("cache write details exceed total writes")
			}
			charge(short, rates.CacheWrite, "5-minute cache write")
			charge(long, rates.CacheWrite1h, "1-hour cache write")
			if u.CacheWrite != nil {
				charge(write-short-long, rates.CacheWrite, "cache write")
			}
		} else {
			charge(write, rates.CacheWrite, "cache write")
		}
	default:
		return invalid("unknown protocol")
	}
	if u.Input != nil {
		charge(input, rates.Input, "input")
	}
	if u.Output != nil {
		charge(output, rates.Output, "output")
	}
	if u.Uncovered {
		result.Reasons = append(result.Reasons, "provider reported fees outside token pricing scope")
	}
	if priced {
		amount := total.FloatString(12)
		result.Amount = &amount
	}
	if missing || len(result.Reasons) > 0 {
		result.Status = "partial"
		if !priced {
			result.Status = "missing_price"
		}
	}
	return result
}
