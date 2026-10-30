package config

import (
	"os"

	"columba.local/backend/internal/atomicfile"
	"github.com/bytedance/sonic"
)

// EnsureConfig creates local defaults only when the configuration is missing.
func EnsureConfig(path string) (*Config, bool, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}

	if err := writeDefaultConfig(path); err != nil {
		return nil, false, err
	}

	cfg, err = Load(path)
	if err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

func writeDefaultConfig(path string) error {
	defaults := map[string]any{
		"host":                "127.0.0.1",
		"port":                8765,
		"databasePath":        "columba.sqlite3",
		"logLevel":            "info",
		"logFormat":           "auto",
		"logPersistLevel":     "info",
		"httpTimeout":         120,
		"secretKeyPath":       ".master-key",
		"logLifecycleVersion": 1,
	}
	out, err := sonic.ConfigStd.MarshalIndent(defaults, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, out, 0o600)
}
