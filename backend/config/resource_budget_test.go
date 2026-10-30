package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequestBudgetReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetMaxBodyBytes() != 32<<20 {
		t.Fatal(cfg.GetMaxBodyBytes())
	}
	pinnedLimit := cfg.GetMaxBodyBytes()
	for _, entry := range []struct {
		body string
		want int64
	}{{`{"maxBodyBytes":1024}`, 1024}, {`{"maxBodyBytes":67108864}`, 64 << 20}} {
		write(entry.body)
		if err := cfg.Reload(); err != nil {
			t.Fatal(err)
		}
		if cfg.GetMaxBodyBytes() != entry.want || pinnedLimit != 32<<20 {
			t.Fatal("reload affected in-flight call")
		}
	}
	write(`{"maxBodyBytes":9223372036854775807}`)
	if err := cfg.Reload(); err == nil {
		t.Fatal("overflow accepted")
	}
	if cfg.GetMaxBodyBytes() != 64<<20 {
		t.Fatal("failed reload published config")
	}
}
