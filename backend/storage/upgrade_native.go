package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// These identifiers belonged to read-only standard presets in schema 2.
func migratedProtocol(value string) string {
	if NativeProtocol(value) {
		return value
	}
	switch strings.TrimPrefix(value, "custom:") {
	case "openai-chat-completions":
		return "openai"
	case "openai-responses":
		return "responses"
	case "anthropic-messages":
		return "claude"
	case "google-generate-content":
		return "gemini"
	}
	return ""
}

func migrateNativeModels(ctx context.Context, tx *sql.Tx) error {
	report := []string{}
	type binding struct {
		Kind, SourceID, ModelID string
		Unbound                 bool
		Binding                 struct {
			ProtocolID string `json:"protocolId"`
		} `json:"binding"`
	}
	type bindingKey struct{ Kind, SourceID, ModelID string }
	bindings := map[bindingKey]string{}
	rows, err := tx.QueryContext(ctx, `SELECT binding FROM protocol_bindings`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw string
		var b binding
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			rows.Close()
			return err
		}
		p := migratedProtocol(b.Binding.ProtocolID)
		if b.Unbound {
			p = ""
		}
		bindings[bindingKey{b.Kind, b.SourceID, b.ModelID}] = p
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	type source struct{ ID, Platform, Manual string }
	sources := []source{}
	rows, err = tx.QueryContext(ctx, `SELECT id,platform,manual_models_json FROM model_sources`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var s source
		if err := rows.Scan(&s.ID, &s.Platform, &s.Manual); err != nil {
			rows.Close()
			return err
		}
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, s := range sources {
		p := migratedProtocol(s.Platform)
		if bound, exists := bindings[bindingKey{"source", s.ID, ""}]; exists {
			if bound != p {
				p = ""
			}
		}
		if p == "" {
			if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET enabled=0,auto_fetch_models=0 WHERE id=?`, s.ID); err != nil {
				return err
			}
			report = append(report, fmt.Sprintf("Channel %s disabled: custom, unbound or conflicting protocol; connection and credentials retained.", s.ID))
			continue
		}
		var manual []map[string]any
		if err := json.Unmarshal([]byte(s.Manual), &manual); err != nil {
			return err
		}
		for _, m := range manual {
			m["platform"] = p
		}
		raw, err := json.Marshal(manual)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE model_sources SET platform=?,manual_models_json=?,cache_synthesis=0 WHERE id=?`, p, string(raw), s.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE models SET platform=? WHERE source_id=?`, p, s.ID); err != nil {
			return err
		}
	}
	for key, p := range bindings {
		if key.Kind != "model" {
			continue
		}
		var sourceProtocol string
		if err := tx.QueryRowContext(ctx, `SELECT platform FROM model_sources WHERE id=?`, key.SourceID).Scan(&sourceProtocol); err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		if p != sourceProtocol || p == "" {
			if _, err := tx.ExecContext(ctx, `UPDATE models SET enabled=0 WHERE source_id=? AND id=?`, key.SourceID, key.ModelID); err != nil {
				return err
			}
			report = append(report, fmt.Sprintf("Model %s/%s disabled: incompatible model protocol binding.", key.SourceID, key.ModelID))
		}
	}
	type group struct {
		ID, Strategy      string
		Retries, Interval int
	}
	groups := []group{}
	rows, err = tx.QueryContext(ctx, `SELECT id,strategy,max_retries,retry_interval FROM model_groups ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.ID, &g.Strategy, &g.Retries, &g.Interval); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	proposals := map[string]ModelRouting{}
	conflicts := map[string]bool{}
	for _, g := range groups {
		rows, err := tx.QueryContext(ctx, `SELECT gm.model_id,gm.source_id,gm.position,COALESCE(m.platform,'') FROM model_group_models gm LEFT JOIN models m ON m.id=gm.model_id AND m.source_id=gm.source_id WHERE gm.group_id=? ORDER BY gm.position`, g.ID)
		if err != nil {
			return err
		}
		var policy ModelRouting
		keys := map[string]bool{}
		valid := true
		for rows.Next() {
			var id, source, wire string
			var position int
			if err := rows.Scan(&id, &source, &position, &wire); err != nil {
				rows.Close()
				return err
			}
			if !NativeProtocol(wire) {
				valid = false
				continue
			}
			key := ModelRoutingKey(id, wire)
			keys[key] = true
			if policy.ModelID == "" {
				policy = DefaultModelRouting(id, wire)
				policy.Strategy = g.Strategy
				if g.Strategy == "sequential" {
					policy.Strategy = "priority"
				}
				if g.Strategy == "random" {
					policy.Strategy = "weighted"
				}
				policy.MaxRetries = g.Retries
				policy.RetryInterval = g.Interval
			}
			policy.Channels[source] = ChannelSchedule{Priority: position, Weight: 1}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(keys) != 1 || !valid || policy.Validate() != nil {
			report = append(report, "Group "+g.ID+": ambiguous models/protocols or unsupported scheduling; default routing retained.")
			for k := range keys {
				conflicts[k] = true
			}
			continue
		}
		key := ModelRoutingKey(policy.ModelID, policy.Protocol)
		if old, exists := proposals[key]; exists && !reflect.DeepEqual(old, policy) {
			conflicts[key] = true
			report = append(report, "Group "+g.ID+": conflicting scheduling for "+key+"; default routing retained.")
		} else {
			proposals[key] = policy
		}
	}
	for key, p := range proposals {
		if conflicts[key] {
			continue
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, "model-routing:"+key, string(raw), nowString()); err != nil {
			return err
		}
		report = append(report, "Migrated scheduling for "+key)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES('migration-report',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, string(raw), nowString()); err != nil {
		return err
	}
	for _, table := range []string{"model_sources", "models"} {
		for _, event := range []string{"insert", "update", "delete"} {
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS conversion_`+table+`_`+event); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"model_group_models", "model_groups", "generation_job_settlements", "generation_jobs", "protocol_bindings", "protocol_history", "protocol_verification_reports", "protocol_activations", "protocol_revisions", "protocol_drafts", "protocol_continuations", "continuation_totals", "conversion_policy_activations", "conversion_policy_drafts", "conversion_policy_evidence", "conversion_policy_revisions", "conversion_provider_evidence", "conversion_generation"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return err
		}
	}
	for _, column := range []string{"allowed_groups_json", "scopes"} {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE api_tokens DROP COLUMN `+column); err != nil {
			return err
		}
	}
	return nil
}
