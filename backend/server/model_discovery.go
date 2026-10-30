package server

import (
	"columba.local/backend/storage"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const maxModelDiscoveryPages = 100

func (s *Server) fetchModelsFromSource(ctx context.Context, source storage.ModelSource, apiKey string) ([]storage.Model, error) {
	if !storage.NativeProtocol(source.Platform) {
		return nil, fmt.Errorf("unsupported native protocol %q", source.Platform)
	}
	ctx, cancel := context.WithTimeout(ctx, modelFetchTimeout)
	defer cancel()
	path := "/v1/models"
	if source.Platform == "gemini" {
		path = "/v1beta/models"
	}
	models := []storage.Model{}
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	query := url.Values{}
	for range maxModelDiscoveryPages {
		target, err := nativeTarget(sourceFetchBase(source), path, query.Encode())
		if err != nil {
			return nil, err
		}
		if err := s.validateOutbound(target); err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		switch source.Platform {
		case "claude":
			request.Header.Set("x-api-key", apiKey)
			request.Header.Set("anthropic-version", "2023-06-01")
		case "gemini":
			request.Header.Set("x-goog-api-key", apiKey)
		default:
			request.Header.Set("Authorization", "Bearer "+apiKey)
		}
		response, err := s.nativeHTTPClient().Do(request)
		if err != nil {
			return nil, fmt.Errorf("model discovery connection failed")
		}
		body, err := readBoundedBody(response.Body, 8<<20)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("model discovery failed: HTTP %d", response.StatusCode)
		}
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Models []struct {
				Name            string `json:"name"`
				DisplayName     string `json:"displayName"`
				InputTokenLimit int    `json:"inputTokenLimit"`
			} `json:"models"`
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("invalid model discovery JSON")
		}
		add := func(id, name string, limit int) error {
			if id == "" || seenIDs[id] {
				return fmt.Errorf("missing or repeated model identity")
			}
			seenIDs[id] = true
			m := inferredModel(source, id, name)
			m.MaxTokens = limit
			models = append(models, m)
			if len(models) > 100000 {
				return fmt.Errorf("model discovery item limit exceeded")
			}
			return nil
		}
		next := ""
		if source.Platform == "gemini" {
			for _, m := range page.Models {
				if err := add(strings.TrimPrefix(m.Name, "models/"), m.DisplayName, m.InputTokenLimit); err != nil {
					return nil, err
				}
			}
			next = page.NextPageToken
			query.Set("pageToken", next)
		} else {
			for _, m := range page.Data {
				if err := add(m.ID, m.ID, 0); err != nil {
					return nil, err
				}
			}
			if source.Platform == "claude" && page.HasMore {
				next = page.LastID
				if next == "" {
					return nil, fmt.Errorf("missing model pagination cursor")
				}
				query.Set("after_id", next)
			}
		}
		if next == "" {
			return models, nil
		}
		if seenCursors[next] {
			return nil, fmt.Errorf("repeated model pagination cursor")
		}
		seenCursors[next] = true
	}
	return nil, fmt.Errorf("model discovery page limit exceeded")
}
