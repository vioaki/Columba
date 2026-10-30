package routing

import (
	"columba.local/backend/storage"
	"sync"
	"testing"
)

func nativeFixture() ([]storage.Model, []storage.ModelSource) {
	models := []storage.Model{}
	sources := []storage.ModelSource{}
	for _, id := range []string{"a", "b", "c"} {
		sources = append(sources, storage.ModelSource{ID: id, Name: id, BaseURL: "https://" + id + ".invalid/v1", Platform: "openai", Enabled: true, APIKeys: []storage.SourceAPIKey{{Value: "first"}, {Value: "second"}, {Value: "first"}}, KeyStrategy: storage.KeyStrategyRoundRobin})
		models = append(models, storage.Model{ID: "exact-model", Name: "Display alias", SourceID: id, Platform: "openai", Enabled: true})
	}
	return models, sources
}

func TestNativeAggregationUsesExactIDProtocolAndEnabledState(t *testing.T) {
	models, sources := nativeFixture()
	models = append(models, storage.Model{ID: "exact-model", SourceID: "a", Platform: "claude", Enabled: true}, storage.Model{ID: "exact-model-preview", SourceID: "a", Platform: "openai", Enabled: true}, storage.Model{ID: "disabled", SourceID: "a", Platform: "openai", Enabled: false})
	sources[2].Enabled = false
	routes := AssembleNative(models, sources, nil)
	if len(routes) != 3 {
		t.Fatal(routes)
	}
	for _, r := range routes {
		if r.ID == "exact-model" && r.Protocol == "openai" && len(r.Channels) != 2 {
			t.Fatal("disabled source or model leaked", r)
		}
		if r.ID == "exact-model" && r.Protocol == "claude" && len(r.Channels) != 1 {
			t.Fatal("protocols mixed", r)
		}
		if r.Routing.MaxRetries != 3 || r.Routing.RetryInterval != 1000 {
			t.Fatal("defaults", r)
		}
	}
	sources[0].APIKeys = []storage.SourceAPIKey{{Value: "disabled", Disabled: true}}
	models[1].Enabled = false
	if got := AssembleNative(models, sources, nil); len(got) != 0 {
		t.Fatal("disabled pool was bypassed", got)
	}
}

func TestNativeSchedulerConcurrentRoundRobinAndUniqueAttempts(t *testing.T) {
	models, sources := nativeFixture()
	r := AssembleNative(models, sources, nil)[0]
	var s NativeScheduler
	var wg sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for range 600 {
		wg.Go(func() {
			attempts := s.Attempts(r)
			if len(attempts) != 6 {
				t.Error("missing keys", len(attempts))
				return
			}
			seen := map[string]bool{}
			for _, a := range attempts {
				k := a.SourceID + a.CredentialID()
				if seen[k] {
					t.Error("duplicate attempt")
				}
				seen[k] = true
			}
			mu.Lock()
			counts[attempts[0].SourceID]++
			mu.Unlock()
		})
	}
	wg.Wait()
	for _, n := range counts {
		if n != 200 {
			t.Fatal("round robin lost concurrency updates", counts)
		}
	}
}

func TestNativeSchedulerPriorityAndWeight(t *testing.T) {
	models, sources := nativeFixture()
	r := AssembleNative(models, sources, nil)[0]
	var s NativeScheduler
	r.Routing.Strategy = "priority"
	r.Routing.Channels = map[string]storage.ChannelSchedule{"a": {Priority: 3, Weight: 1}, "b": {Priority: 1, Weight: 3}, "c": {Priority: 2, Weight: 2}}
	if got := s.Attempts(r); got[0].SourceID != "b" || got[2].SourceID != "c" {
		t.Fatal(got)
	}
	r.Routing.Strategy = "weighted"
	counts := map[string]int{}
	for range 600 {
		counts[s.Attempts(r)[0].SourceID]++
	}
	if counts["a"] != 100 || counts["b"] != 300 || counts["c"] != 200 {
		t.Fatal(counts)
	}
	s.Reset()
	r.Channels = r.Channels[:1]
	if len(s.Attempts(r)) != 2 {
		t.Fatal("deleted channel retained")
	}
}
