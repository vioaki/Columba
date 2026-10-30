package storage

import "testing"

func TestLatestUpgradeMapsNativeChannelsAndOnlyUnambiguousSchedules(t *testing.T) {
	path, logs, key := previousDatabases(t)
	db, err := openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO model_sources(id,name,base_url,api_key,platform,created_at,updated_at) VALUES ('a','a','https://a.example','preserved','custom:openai-chat-completions','',''),('b','b','https://b.example','','custom:anthropic-messages','',''),('x','x','https://custom.example','custom-secret','custom:private','','')`,
		`INSERT INTO models(id,source_id,name,base_url,platform,last_checked_at) VALUES('same','a','same','https://a.example','custom:openai-chat-completions',''),('same','b','same','https://b.example','custom:anthropic-messages',''),('one','a','one','https://a.example','custom:openai-chat-completions',''),('two','a','two','https://a.example','custom:openai-chat-completions','')`,
		`INSERT INTO model_groups(id,name,strategy,max_retries,retry_interval,created_at,updated_at) VALUES('ok','old alias','sequential',1,25,'',''),('mixed','mixed','random',9,9,'',''),('conflict-a','a','round-robin',1,1,'',''),('conflict-b','b','round-robin',2,2,'','')`,
		`INSERT INTO model_group_models(group_id,model_id,source_id) VALUES('ok','one','a'),('mixed','same','a'),('mixed','same','b'),('conflict-a','two','a'),('conflict-b','two','a')`,
		`UPDATE api_tokens SET allowed_groups_json='["old alias"]'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	codec, err := newSecretCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	for id, value := range map[string]string{"a": "preserved", "x": "custom-secret"} {
		encrypted, err := codec.encrypt(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE model_sources SET api_key=? WHERE id=?`, encrypted, id); err != nil {
			t.Fatal(err)
		}
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
	sources, err := s.ListSources(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range sources {
		switch src.ID {
		case "a":
			if src.Platform != "openai" || !src.Enabled || src.APIKey != "preserved" {
				t.Fatal(src)
			}
		case "b":
			if src.Platform != "claude" || !src.Enabled {
				t.Fatal(src)
			}
		case "x":
			if src.Enabled || src.APIKey != "custom-secret" || src.BaseURL != "https://custom.example" {
				t.Fatal(src)
			}
		}
	}
	policies, err := s.ListModelRouting(t.Context())
	if err != nil || len(policies) != 1 {
		t.Fatal(policies, err)
	}
	p := policies[0]
	if p.ModelID != "one" || p.Protocol != "openai" || p.Strategy != "priority" || p.MaxRetries != 1 || p.RetryInterval != 25 {
		t.Fatal(p)
	}
	var report []string
	found, err := s.GetSetting(t.Context(), "migration-report", &report)
	if err != nil || !found || len(report) < 3 {
		t.Fatal(report, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='model_groups' OR name LIKE 'protocol_%' OR name LIKE 'conversion_%'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retired schema survived", count, err)
	}
	// Retained model/source writes must not reference deleted conversion-generation triggers.
	if _, err := s.UpdateModel(t.Context(), "one", "a", ModelPatch{Enabled: new(bool)}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownCurrentFormatIsRejectedBeforeSchemaRepair(t *testing.T) {
	path, logs, _ := previousDatabases(t)
	db, err := openUpgradeDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE models`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := UpgradeDatabases(path, logs, ""); err == nil {
		t.Fatal("incomplete schema accepted")
	}
}
