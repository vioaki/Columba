CREATE TABLE api_tokens (name TEXT PRIMARY KEY, token TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, allowed_groups_json TEXT NOT NULL DEFAULT '[]', token_hash TEXT NOT NULL DEFAULT '', scopes TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE continuation_totals (id INTEGER PRIMARY KEY CHECK(id=1), bytes INTEGER NOT NULL, records INTEGER NOT NULL, hits INTEGER NOT NULL DEFAULT 0, misses INTEGER NOT NULL DEFAULT 0, evicted INTEGER NOT NULL DEFAULT 0);
CREATE TABLE conversion_generation (id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL);
CREATE TABLE conversion_policy_activations (id TEXT PRIMARY KEY, hash TEXT NOT NULL, selector TEXT NOT NULL, generation INTEGER NOT NULL DEFAULT 1);
CREATE TABLE conversion_policy_drafts (id TEXT PRIMARY KEY, hash TEXT NOT NULL, document TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE conversion_policy_evidence (sequence INTEGER PRIMARY KEY AUTOINCREMENT, policy_id TEXT NOT NULL, revision_hash TEXT NOT NULL, compiler TEXT NOT NULL, reports TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE conversion_policy_revisions (id TEXT NOT NULL, hash TEXT NOT NULL, document TEXT NOT NULL, reports TEXT NOT NULL, compiler_version TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(id,hash));
CREATE TABLE conversion_provider_evidence (policy_hash TEXT NOT NULL, rule_id TEXT NOT NULL, scope_key TEXT NOT NULL, target_hash TEXT NOT NULL, compiler TEXT NOT NULL, expires_at INTEGER NOT NULL, PRIMARY KEY(policy_hash,rule_id,scope_key,target_hash,compiler));
CREATE TABLE generation_job_settlements (
		 job_id TEXT PRIMARY KEY,settlement_id TEXT NOT NULL UNIQUE,is_delivered INTEGER NOT NULL DEFAULT 0,
		 FOREIGN KEY(job_id) REFERENCES generation_jobs(id));
CREATE TABLE generation_jobs (
		 id TEXT PRIMARY KEY,owner_hash TEXT NOT NULL,deduplication_hash TEXT NOT NULL,
		 phase TEXT NOT NULL,revision INTEGER NOT NULL,next_attempt INTEGER NOT NULL,
		 lease_until INTEGER NOT NULL,job TEXT NOT NULL,result TEXT NOT NULL,
		 UNIQUE(owner_hash,deduplication_hash));
CREATE TABLE model_group_models (
    group_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (group_id, model_id, source_id),
    FOREIGN KEY (group_id) REFERENCES model_groups(id) ON DELETE CASCADE
);
CREATE TABLE model_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    strategy TEXT NOT NULL DEFAULT 'round-robin',
    max_retries INTEGER NOT NULL DEFAULT 3,
    retry_interval INTEGER NOT NULL DEFAULT 1000,
    max_concurrency INTEGER NOT NULL DEFAULT 0,
    daily_limit_max_requests INTEGER NOT NULL DEFAULT 0,
    daily_limit_max_tokens INTEGER NOT NULL DEFAULT 0,
    type TEXT NOT NULL DEFAULT 'llm',
    max_tokens INTEGER NOT NULL DEFAULT 0,
    vision_capable INTEGER NOT NULL DEFAULT 0,
    tools_capable INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE model_sources (id TEXT PRIMARY KEY, name TEXT NOT NULL, base_url TEXT NOT NULL, api_key TEXT NOT NULL DEFAULT '', platform TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, auto_fetch_models INTEGER NOT NULL DEFAULT 1, manual_models_json TEXT NOT NULL DEFAULT '[]', fetch_base_url TEXT NOT NULL DEFAULT '', api_keys TEXT NOT NULL DEFAULT '', key_strategy TEXT NOT NULL DEFAULT 'single', cache_synthesis INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE models (id TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, source_name TEXT NOT NULL DEFAULT '', base_url TEXT NOT NULL, api_key TEXT NOT NULL DEFAULT '', platform TEXT NOT NULL, type TEXT NOT NULL DEFAULT 'llm', max_tokens INTEGER NOT NULL DEFAULT 0, vision_capable INTEGER NOT NULL DEFAULT 0, tools_capable INTEGER NOT NULL DEFAULT 0, structured_output INTEGER NOT NULL DEFAULT 0, thinking_mode TEXT NOT NULL DEFAULT 'both', available INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL DEFAULT 1, origin TEXT NOT NULL DEFAULT 'fetched', capability_source TEXT NOT NULL DEFAULT '', last_checked_at TEXT NOT NULL, PRIMARY KEY (id, source_id));
CREATE TABLE protocol_activations (
			protocol_id TEXT PRIMARY KEY, revision_hash TEXT NOT NULL, generation INTEGER NOT NULL,
			activated_at TEXT NOT NULL,
			FOREIGN KEY(protocol_id, revision_hash) REFERENCES protocol_revisions(protocol_id, content_hash));
CREATE TABLE protocol_bindings (binding_key TEXT PRIMARY KEY, binding TEXT NOT NULL);
CREATE TABLE protocol_continuations (id TEXT PRIMARY KEY, owner TEXT NOT NULL, session TEXT NOT NULL, scope_key TEXT NOT NULL, parent TEXT NOT NULL, ordinal INTEGER NOT NULL, digest TEXT NOT NULL, ciphertext TEXT NOT NULL, bytes INTEGER NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL);
CREATE TABLE protocol_drafts (
			protocol_id TEXT PRIMARY KEY, content_hash TEXT NOT NULL,
			definition TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE protocol_history (
			id TEXT PRIMARY KEY, protocol_id TEXT NOT NULL, content_hash TEXT NOT NULL,
			definition TEXT NOT NULL, reason TEXT NOT NULL, is_draft INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, archived_at TEXT NOT NULL);
CREATE TABLE protocol_revisions (
			protocol_id TEXT NOT NULL, content_hash TEXT NOT NULL, definition TEXT NOT NULL,
			created_at TEXT NOT NULL, PRIMARY KEY(protocol_id, content_hash));
CREATE TABLE protocol_verification_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT, protocol_id TEXT NOT NULL, revision_hash TEXT NOT NULL,
			compiler_version TEXT NOT NULL, samples_hash TEXT NOT NULL, kind TEXT NOT NULL,
			report TEXT NOT NULL, verified_at TEXT NOT NULL,
			FOREIGN KEY(protocol_id, revision_hash) REFERENCES protocol_revisions(protocol_id, content_hash));
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_api_tokens_hash ON api_tokens(token_hash) WHERE token_hash != '';
CREATE INDEX idx_continuation_eviction ON protocol_continuations(created_at,id);
CREATE INDEX idx_continuation_expiry ON protocol_continuations(expires_at);
CREATE INDEX idx_continuation_match ON protocol_continuations(owner,session,scope_key,parent,ordinal,digest);
CREATE INDEX idx_continuation_session ON protocol_continuations(owner,session,scope_key,parent,created_at);
CREATE INDEX idx_conversion_policy_evidence ON conversion_policy_evidence(policy_id,revision_hash,sequence);
CREATE INDEX idx_generation_jobs_due ON generation_jobs(phase,next_attempt,lease_until);
CREATE INDEX idx_protocol_history_time ON protocol_history(archived_at DESC,id);
CREATE INDEX idx_protocol_reports_revision
			ON protocol_verification_reports(protocol_id, revision_hash, compiler_version, kind, id);
PRAGMA user_version=2;
CREATE TRIGGER continuation_delete AFTER DELETE ON protocol_continuations BEGIN UPDATE continuation_totals SET bytes=bytes-OLD.bytes,records=records-1 WHERE id=1; END;
CREATE TRIGGER continuation_insert AFTER INSERT ON protocol_continuations BEGIN UPDATE continuation_totals SET bytes=bytes+NEW.bytes,records=records+1 WHERE id=1; END;
CREATE TRIGGER conversion_active_delete AFTER DELETE ON conversion_policy_activations BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_active_insert AFTER INSERT ON conversion_policy_activations BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_active_update AFTER UPDATE ON conversion_policy_activations BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_binding_delete AFTER DELETE ON protocol_bindings BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_binding_insert AFTER INSERT ON protocol_bindings BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_binding_update AFTER UPDATE ON protocol_bindings BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_groups_delete AFTER DELETE ON model_groups BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_groups_insert AFTER INSERT ON model_groups BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_groups_update AFTER UPDATE ON model_groups BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_sources_delete AFTER DELETE ON model_sources BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_sources_insert AFTER INSERT ON model_sources BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_model_sources_update AFTER UPDATE ON model_sources BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_models_delete AFTER DELETE ON models BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_models_insert AFTER INSERT ON models BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
CREATE TRIGGER conversion_models_update AFTER UPDATE ON models BEGIN UPDATE conversion_generation SET generation=generation+1 WHERE id=1; END;
