package storage

import (
	"path/filepath"
	"testing"
)

func TestModelRoutingPersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := DefaultModelRouting("provider/model:exact", "openai")
	p.Strategy = "weighted"
	p.Channels["channel"] = ChannelSchedule{Priority: 2, Weight: 3}
	if err := s.SaveModelRouting(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	p.Protocol = "claude"
	p.MaxRetries = 0
	p.RetryInterval = 0
	if err := s.SaveModelRouting(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.ListModelRouting(t.Context())
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	p.MaxRetries = -1
	if err := s.SaveModelRouting(t.Context(), p); err == nil {
		t.Fatal("invalid retries accepted")
	}
	p.MaxRetries = 3
	p.Channels["channel"] = ChannelSchedule{Weight: 0}
	if err := s.SaveModelRouting(t.Context(), p); err == nil {
		t.Fatal("zero weight accepted")
	}
	p.Channels = nil
	p.Protocol = "custom:guess"
	if err := s.SaveModelRouting(t.Context(), p); err == nil {
		t.Fatal("custom protocol accepted")
	}
}
