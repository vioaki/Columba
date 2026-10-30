package billing

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

// Rate is USD per million tokens; decimal text avoids binary floating-point money.
type Rate string

func (r Rate) Rat() (*big.Rat, error) {
	if len(r) == 0 || len(r) > 40 {
		return nil, fmt.Errorf("invalid price %q", r)
	}
	n, ok := new(big.Rat).SetString(string(r))
	if !ok || n.Sign() < 0 || strings.ContainsAny(string(r), "/eE") {
		return nil, fmt.Errorf("price must be a nonnegative decimal: %q", r)
	}
	return n, nil
}
func (r *Rate) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	} else {
		value = string(raw)
	}
	rate := Rate(value)
	if _, err := rate.Rat(); err != nil {
		return err
	}
	*r = rate
	return nil
}

type Rates struct {
	Input        *Rate `json:"input,omitempty"`
	Output       *Rate `json:"output,omitempty"`
	Reasoning    *Rate `json:"reasoning,omitempty"`
	CacheRead    *Rate `json:"cache_read,omitempty"`
	CacheWrite   *Rate `json:"cache_write,omitempty"`
	CacheWrite1h *Rate `json:"cache_write_1h,omitempty"`
}

func (r Rates) Validate() error {
	for _, v := range []*Rate{r.Input, r.Output, r.Reasoning, r.CacheRead, r.CacheWrite, r.CacheWrite1h} {
		if v != nil {
			if _, err := v.Rat(); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r Rates) Overlay(o Rates) Rates {
	if o.Input != nil {
		r.Input = o.Input
	}
	if o.Output != nil {
		r.Output = o.Output
	}
	if o.Reasoning != nil {
		r.Reasoning = o.Reasoning
	}
	if o.CacheRead != nil {
		r.CacheRead = o.CacheRead
	}
	if o.CacheWrite != nil {
		r.CacheWrite = o.CacheWrite
	}
	if o.CacheWrite1h != nil {
		r.CacheWrite1h = o.CacheWrite1h
	}
	return r
}

type Tier struct {
	Rates
	Tier struct {
		Type string `json:"type"`
		Size int64  `json:"size"`
	} `json:"tier"`
}
type Prices struct {
	Rates
	Tiers       []Tier `json:"tiers"`
	Legacy200k  *Rates `json:"context_over_200k,omitempty"`
	InputAudio  *Rate  `json:"input_audio,omitempty"`
	OutputAudio *Rate  `json:"output_audio,omitempty"`
}

func (p Prices) Validate() error {
	if err := p.Rates.Validate(); err != nil {
		return err
	}
	last := int64(0)
	for _, t := range p.Tiers {
		if t.Tier.Type != "context" || t.Tier.Size <= last {
			return fmt.Errorf("context tiers must have increasing positive thresholds")
		}
		last = t.Tier.Size
		if err := t.Rates.Validate(); err != nil {
			return err
		}
	}
	if p.Legacy200k != nil {
		return p.Legacy200k.Validate()
	}
	return nil
}
func (p Prices) Normalize() Prices {
	if len(p.Tiers) == 0 && p.Legacy200k != nil {
		t := Tier{Rates: *p.Legacy200k}
		t.Tier.Type = "context"
		t.Tier.Size = 200000
		p.Tiers = []Tier{t}
	}
	p.Legacy200k = nil
	sort.Slice(p.Tiers, func(i, j int) bool { return p.Tiers[i].Tier.Size < p.Tiers[j].Tier.Size })
	return p
}
func (p Prices) Overlay(o Prices) Prices {
	p.Rates = p.Rates.Overlay(o.Rates)
	// Base manual rates override the corresponding fields in every catalog tier.
	p.Tiers = append([]Tier(nil), p.Tiers...)
	for i := range p.Tiers {
		p.Tiers[i].Rates = p.Tiers[i].Rates.Overlay(o.Rates)
	}
	if o.Tiers != nil {
		p.Tiers = append([]Tier(nil), o.Tiers...)
	}
	return p
}
