package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"sort"
	"sync"

	"columba.local/backend/storage"
)

// NativeModel is an automatically aggregated model, never a user-defined alias.
type NativeModel struct {
	ID       string               `json:"id"`
	Protocol string               `json:"protocol"`
	Routing  storage.ModelRouting `json:"routing"`
	Channels []NativeChannel      `json:"channels"`
}

type NativeChannel struct {
	SourceID    string                    `json:"sourceId"`
	SourceName  string                    `json:"sourceName"`
	BaseURL     string                    `json:"-"`
	Keys        []string                  `json:"-"`
	KeyStrategy storage.SourceKeyStrategy `json:"-"`
}

type NativeAttempt struct {
	ModelID, Protocol, SourceID, SourceName, BaseURL, APIKey string
}

func (a NativeAttempt) CredentialID() string {
	hash := sha256.Sum256([]byte(a.APIKey))
	return hex.EncodeToString(hash[:])
}

// AssembleNative uses only enabled channels and models. Availability from retired
// generation probes does not hide a model. Credentials always come from channels.
func AssembleNative(models []storage.Model, sources []storage.ModelSource, policies []storage.ModelRouting) []NativeModel {
	sourceIndex := map[string]storage.ModelSource{}
	for _, s := range sources {
		if s.Enabled && s.BaseURL != "" {
			sourceIndex[s.ID] = s
		}
	}
	policyIndex := map[string]storage.ModelRouting{}
	for _, p := range policies {
		policyIndex[storage.ModelRoutingKey(p.ModelID, p.Protocol)] = p
	}
	result := []NativeModel{}
	indexes := map[string]int{}
	for _, m := range models {
		s, exists := sourceIndex[m.SourceID]
		if !exists || !m.Enabled || !storage.NativeProtocol(s.Platform) {
			continue
		}
		keys := []string{}
		seen := map[string]bool{}
		for _, k := range s.EffectiveKeys() {
			if k.KeyAllowsModel(m.ID) && !seen[k.Value] {
				keys = append(keys, k.Value)
				seen[k.Value] = true
			}
		}
		if len(keys) == 0 {
			if len(s.APIKeys) > 0 || s.APIKey != "" {
				continue
			}
			keys = []string{""}
		}
		m.Platform = s.Platform
		key := storage.ModelRoutingKey(m.ID, m.Platform)
		index, exists := indexes[key]
		if !exists {
			index = len(result)
			indexes[key] = index
			p, ok := policyIndex[key]
			if !ok {
				p = storage.DefaultModelRouting(m.ID, m.Platform)
			}
			result = append(result, NativeModel{ID: m.ID, Protocol: m.Platform, Routing: p, Channels: []NativeChannel{}})
		}
		result[index].Channels = append(result[index].Channels, NativeChannel{SourceID: s.ID, SourceName: s.Name, BaseURL: s.BaseURL, Keys: keys, KeyStrategy: s.KeyStrategy})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Protocol < result[j].Protocol
	})
	for i := range result {
		sort.Slice(result[i].Channels, func(a, b int) bool { return result[i].Channels[a].SourceID < result[i].Channels[b].SourceID })
	}
	return result
}

type NativeScheduler struct {
	mu      sync.Mutex
	cursors map[string]uint64
	scores  map[string]map[string]int64
}

func (s *NativeScheduler) Reset() { s.mu.Lock(); defer s.mu.Unlock(); s.cursors = nil; s.scores = nil }

func (s *NativeScheduler) Attempts(model NativeModel) []NativeAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursors == nil {
		s.cursors = map[string]uint64{}
	}
	if s.scores == nil {
		s.scores = map[string]map[string]int64{}
	}
	channels := append([]NativeChannel(nil), model.Channels...)
	if len(channels) == 0 {
		return nil
	}
	key := storage.ModelRoutingKey(model.ID, model.Protocol)
	switch model.Routing.Strategy {
	case "priority":
		sort.SliceStable(channels, func(i, j int) bool {
			return model.Routing.Channels[channels[i].SourceID].Priority < model.Routing.Channels[channels[j].SourceID].Priority
		})
	case "weighted":
		scores := s.scores[key]
		if scores == nil {
			scores = map[string]int64{}
			s.scores[key] = scores
		}
		selected, total := 0, int64(0)
		for i, c := range channels {
			w := model.Routing.Channels[c.SourceID].Weight
			if w < 1 {
				w = 1
			}
			total += int64(w)
			scores[c.SourceID] += int64(w)
			if scores[c.SourceID] > scores[channels[selected].SourceID] {
				selected = i
			}
		}
		scores[channels[selected].SourceID] -= total
		channels = append(channels[selected:], channels[:selected]...)
	default:
		start := int(s.cursors[key] % uint64(len(channels)))
		s.cursors[key]++
		channels = append(channels[start:], channels[:start]...)
	}
	attempts := []NativeAttempt{}
	for _, c := range channels {
		keys := append([]string(nil), c.Keys...)
		if c.KeyStrategy == storage.KeyStrategyRandom {
			rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		} else if len(keys) > 1 && c.KeyStrategy != storage.KeyStrategyPriority && c.KeyStrategy != storage.KeyStrategySingle {
			cursorKey := "key:" + c.SourceID
			start := int(s.cursors[cursorKey] % uint64(len(keys)))
			s.cursors[cursorKey]++
			keys = append(keys[start:], keys[:start]...)
		}
		for _, credential := range keys {
			attempts = append(attempts, NativeAttempt{ModelID: model.ID, Protocol: model.Protocol, SourceID: c.SourceID, SourceName: c.SourceName, BaseURL: c.BaseURL, APIKey: credential})
		}
	}
	return attempts
}

func MaxAttempts(maxRetries, candidates int) int { return min(max(0, maxRetries)+1, candidates) }
