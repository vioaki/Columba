package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ModelRouting belongs to one exact upstream model ID and one native protocol.
type ModelRouting struct {
	ModelID       string                     `json:"modelId"`
	Protocol      string                     `json:"protocol"`
	Strategy      string                     `json:"strategy"`
	MaxRetries    int                        `json:"maxRetries"`
	RetryInterval int                        `json:"retryInterval"`
	Channels      map[string]ChannelSchedule `json:"channels"`
}

type ChannelSchedule struct {
	Priority int `json:"priority"`
	Weight   int `json:"weight"`
}

func NativeProtocol(value string) bool {
	switch value {
	case "openai", "responses", "claude", "gemini":
		return true
	}
	return false
}

func ModelRoutingKey(model, protocol string) string {
	key, _ := json.Marshal([]string{protocol, model})
	return string(key)
}

func DefaultModelRouting(model, protocol string) ModelRouting {
	return ModelRouting{ModelID: model, Protocol: protocol, Strategy: "round-robin", MaxRetries: 3, RetryInterval: 1000, Channels: map[string]ChannelSchedule{}}
}

func (p ModelRouting) Validate() error {
	if strings.TrimSpace(p.ModelID) == "" || !NativeProtocol(p.Protocol) {
		return errors.New("exact modelId and native protocol are required")
	}
	switch p.Strategy {
	case "round-robin", "priority", "weighted":
	default:
		return errors.New("strategy must be round-robin, priority or weighted")
	}
	if p.MaxRetries < 0 || p.MaxRetries > 100 || p.RetryInterval < 0 || p.RetryInterval > 60000 {
		return errors.New("maxRetries must be 0..100 and retryInterval must be 0..60000 ms")
	}
	for source, ch := range p.Channels {
		if source == "" || ch.Priority < 0 || ch.Priority > 100000 || ch.Weight < 1 || ch.Weight > 1000 {
			return fmt.Errorf("invalid schedule for channel %q", source)
		}
	}
	return nil
}

func (s *Store) ListModelRouting(ctx context.Context) ([]ModelRouting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT value FROM settings WHERE key LIKE 'model-routing:%' ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := []ModelRouting{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p ModelRouting
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, rows.Err()
}

func (s *Store) SaveModelRouting(ctx context.Context, p ModelRouting) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, "model-routing:"+ModelRoutingKey(p.ModelID, p.Protocol), string(raw), nowString())
	return err
}
