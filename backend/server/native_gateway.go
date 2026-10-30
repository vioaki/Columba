package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"columba.local/backend/internal/nativeusage"
	"columba.local/backend/internal/outbound"
	"columba.local/backend/internal/routing"
	"columba.local/backend/storage"
	"github.com/cloudwego/hertz/pkg/app"
)

type nativeAttemptRecord struct {
	Attempt   int               `json:"attempt"`
	SourceID  string            `json:"sourceId"`
	ModelID   string            `json:"modelId"`
	Protocol  string            `json:"protocol"`
	Status    int               `json:"status"`
	Usage     nativeusage.Usage `json:"usage"`
	Anomalies []string          `json:"anomalies,omitempty"`
}

func (s *Server) nativeModels(ctx context.Context) ([]routing.NativeModel, error) {
	models, err := s.store.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := s.store.ListModelRouting(ctx)
	if err != nil {
		return nil, err
	}
	return routing.AssembleNative(models, sources, policies), nil
}

func nativeWire(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1beta/"):
		return "gemini"
	case strings.HasPrefix(path, "/v1/messages"):
		return "claude"
	case path == "/v1/responses":
		return "responses"
	default:
		return "openai"
	}
}

func nativeError(c *app.RequestContext, status int, message string) {
	switch nativeWire(string(c.Path())) {
	case "claude":
		c.JSON(status, map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": message}})
	case "gemini":
		c.JSON(status, map[string]any{"error": map[string]any{"code": status, "message": message, "status": http.StatusText(status)}})
	default:
		c.JSON(status, map[string]any{"error": map[string]string{"message": message, "type": "invalid_request_error"}})
	}
}

func nativeTarget(base, path, query string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid channel base URL")
	}
	prefix := "/v1"
	if strings.HasPrefix(path, "/v1beta/") {
		prefix = "/v1beta"
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(basePath, prefix) {
		path = strings.TrimPrefix(path, prefix)
	}
	u.Path = basePath + path
	u.RawPath = ""
	values := u.Query()
	incoming, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	for k, v := range incoming {
		values[k] = v
	}
	values.Del("key")
	u.RawQuery = values.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func hopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "content-length":
		return true
	}
	return false
}

func connectionHeaders(header string) map[string]bool {
	m := map[string]bool{}
	for _, key := range strings.Split(header, ",") {
		m[strings.ToLower(strings.TrimSpace(key))] = true
	}
	return m
}

func (s *Server) nativeHTTPClient() *http.Client {
	s.nativeTransportOnce.Do(func() { s.nativeTransport = outbound.NewSecureTransport() })
	return &http.Client{Transport: s.nativeTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func nativeStateIDs(raw map[string]json.RawMessage) ([]string, error) {
	ids := []string{}
	for _, key := range []string{"previous_response_id", "conversation", "container", "cachedContent"} {
		value, ok := raw[key]
		if !ok || string(value) == "null" {
			continue
		}
		var id string
		if err := json.Unmarshal(value, &id); err != nil {
			var object struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(value, &object) != nil || object.ID == "" {
				return nil, fmt.Errorf("cannot determine channel ownership for %s", key)
			}
			id = object.ID
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Server) serveNative(ctx context.Context, c *app.RequestContext) {
	path := string(c.Path())
	wire := nativeWire(path)
	limit := s.config.GetMaxBodyBytes()
	if limit <= 0 {
		limit = 64 << 20
	}
	body, err := io.ReadAll(io.LimitReader(requestBody(c), limit+1))
	if err != nil {
		nativeError(c, 400, "cannot read request body")
		return
	}
	if int64(len(body)) > limit {
		nativeError(c, 413, "request body is too large")
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || envelope == nil {
		nativeError(c, 400, "request must be a JSON object")
		return
	}
	var modelID string
	var streaming bool
	_ = json.Unmarshal(envelope["model"], &modelID)
	_ = json.Unmarshal(envelope["stream"], &streaming)
	if wire == "gemini" {
		action := strings.TrimPrefix(path, "/v1beta/models/")
		separator := strings.LastIndexByte(action, ':')
		if separator < 1 {
			nativeError(c, 400, "invalid Gemini model action")
			return
		}
		modelID = action[:separator]
		switch action[separator+1:] {
		case "generateContent", "countTokens":
		case "streamGenerateContent":
			streaming = true
		default:
			nativeError(c, 404, "unsupported Gemini action")
			return
		}
	}
	if modelID == "" {
		nativeError(c, 400, "model ID is required")
		return
	}
	models, err := s.nativeModels(ctx)
	if err != nil {
		nativeError(c, 503, "cannot load model channels")
		return
	}
	var model *routing.NativeModel
	for i := range models {
		if models[i].ID == modelID && models[i].Protocol == wire {
			model = &models[i]
			break
		}
	}
	if model == nil {
		nativeError(c, 404, "no enabled channel for this exact model ID and protocol")
		return
	}
	candidates := s.nativeScheduler.Attempts(*model)
	stateIDs, err := nativeStateIDs(envelope)
	if err != nil {
		nativeError(c, 409, err.Error())
		return
	}
	hash := sha256.Sum256([]byte(extractAccessToken(c)))
	clientHash := hex.EncodeToString(hash[:])
	for _, id := range stateIDs {
		owner, found, err := s.store.FindNativeContinuation(ctx, wire, clientHash, id)
		if err != nil {
			nativeError(c, 503, "cannot read continuation ownership")
			return
		}
		if !found || owner.SourceID == "" || owner.ModelID != modelID {
			nativeError(c, 409, "unknown or ambiguous continuation ownership; start a new request without upstream state")
			return
		}
		kept := candidates[:0]
		for _, candidate := range candidates {
			if candidate.SourceID == owner.SourceID && candidate.CredentialID() == owner.CredentialID && candidate.BaseURL == owner.BaseURL {
				kept = append(kept, candidate)
			}
		}
		candidates = kept
	}
	if len(candidates) == 0 {
		nativeError(c, 409, "the continuation channel or credential is no longer available")
		return
	}
	maxAttempts := routing.MaxAttempts(model.Routing.MaxRetries, len(candidates))
	if len(stateIDs) > 0 {
		maxAttempts = 1
	}
	record := s.initUsageRecord(ctx, c, time.Now(), body, inputFormatFromPath(path))
	record.ModelID = modelID
	record.ModelName = modelID
	record.Platform = wire
	record.Stream = streaming
	record.RelayMode = "passthrough"
	record.RequestedModelGroup = modelID
	defer func() {
		record.EndedAt = time.Now()
		record.DurationMs = record.EndedAt.Sub(record.StartedAt).Milliseconds()
		s.recordHTTPUsage(c, record)
	}()
	for index, candidate := range candidates[:maxAttempts] {
		if ctx.Err() != nil {
			record.StatusCode = 499
			record.Error = "client canceled"
			return
		}
		if index > 0 && !waitForRetryOrCancel(ctx, model.Routing.RetryInterval) {
			record.StatusCode = 499
			record.Error = "client canceled"
			return
		}
		target, err := nativeTarget(candidate.BaseURL, path, string(c.URI().QueryString()))
		if err != nil {
			record.StatusCode = 400
			record.Error = err.Error()
			nativeError(c, 400, err.Error())
			return
		}
		if err := s.validateOutbound(target); err != nil {
			record.StatusCode = 400
			record.Error = "channel URL rejected"
			nativeError(c, 400, record.Error)
			return
		}
		requestCtx, cancel := context.WithCancel(ctx)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			cancel()
			nativeError(c, 400, "invalid upstream request")
			return
		}
		request.GetBody = nil // Never replay POST after an uncertain transport failure.
		blocked := connectionHeaders(string(c.GetHeader("Connection")))
		c.Request.Header.VisitAll(func(k, v []byte) {
			key := strings.ToLower(string(k))
			if hopHeader(key) || blocked[key] {
				return
			}
			switch key {
			case "authorization", "x-api-key", "x-goog-api-key", "host", "cookie", "accept-encoding":
				return
			}
			request.Header.Add(string(k), string(v))
		})
		request.Header.Set("Accept-Encoding", "identity")
		switch wire {
		case "claude":
			if candidate.APIKey != "" {
				request.Header.Set("x-api-key", candidate.APIKey)
			}
			if request.Header.Get("anthropic-version") == "" {
				request.Header.Set("anthropic-version", "2023-06-01")
			}
		case "gemini":
			if candidate.APIKey != "" {
				request.Header.Set("x-goog-api-key", candidate.APIKey)
			}
		default:
			if candidate.APIKey != "" {
				request.Header.Set("Authorization", "Bearer "+candidate.APIKey)
			}
		}
		timeout := time.Duration(s.config.GetHTTPTimeout()) * time.Second
		var timer *time.Timer
		if timeout > 0 {
			timer = time.AfterFunc(timeout, cancel)
		}
		response, sendErr := s.nativeHTTPClient().Do(request)
		if timer != nil {
			timer.Stop()
		}
		attempt := nativeAttemptRecord{Attempt: index + 1, SourceID: candidate.SourceID, ModelID: modelID, Protocol: wire}
		record.SourceID = candidate.SourceID
		record.RetryCount = index
		if sendErr != nil {
			cancel()
			record.StatusCode = 502
			record.Error = "upstream transport failed; request may have been sent"
			if ctx.Err() != nil {
				record.StatusCode = 499
				record.Error = "client canceled"
			}
			attempt.Anomalies = []string{record.Error}
			record.NativeAttempts = append(record.NativeAttempts, attempt)
			nativeError(c, record.StatusCode, record.Error)
			return
		}
		observer := nativeusage.Observer{Protocol: wire, SSE: strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")}
		attempt.Status = response.StatusCode
		retry := index+1 < maxAttempts && nativeRetryStatus(response.StatusCode) && len(stateIDs) == 0
		if !retry {
			blocked := connectionHeaders(response.Header.Get("Connection"))
			for key, values := range response.Header {
				if hopHeader(key) || blocked[strings.ToLower(key)] {
					continue
				}
				for _, value := range values {
					c.Response.Header.Add(key, value)
				}
			}
			c.Status(response.StatusCode)
		}
		var writer io.Writer
		if !retry {
			writer = startStream(c)
		}
		buffer := make([]byte, 32<<10)
		var copyErr error
		for {
			if timer != nil {
				timer.Reset(timeout)
			}
			count, readErr := response.Body.Read(buffer)
			if timer != nil {
				timer.Stop()
			}
			if count > 0 {
				observer.Write(buffer[:count])
				if writer != nil {
					if record.FirstByteMs == 0 {
						record.FirstByteMs = time.Since(record.StartedAt).Milliseconds()
					}
					if _, copyErr = writer.Write(buffer[:count]); copyErr != nil {
						break
					}
					if stream := currentResponseStream(c); stream != nil {
						if copyErr = stream.Flush(); copyErr != nil {
							break
						}
					}
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					copyErr = readErr
				}
				break
			}
			if requestCtx.Err() != nil {
				copyErr = requestCtx.Err()
				break
			}
		}
		response.Body.Close()
		if timer != nil {
			timer.Stop()
		}
		cancel()
		observer.Finish()
		attempt.Usage, attempt.Anomalies = observer.Usage, observer.Anomalies
		record.NativeAttempts = append(record.NativeAttempts, attempt)
		record.StatusCode = response.StatusCode
		record.UpstreamHTTPStatus = response.StatusCode
		addNativeUsage(record, observer.Usage)
		record.RequestWarnings = append(record.RequestWarnings, observer.Anomalies...)
		owner := storage.NativeContinuation{Protocol: wire, ClientHash: clientHash, ModelID: modelID, SourceID: candidate.SourceID, CredentialID: candidate.CredentialID(), BaseURL: candidate.BaseURL}
		if len(observer.IDs) > 0 {
			persistCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.store.SaveNativeContinuations(persistCtx, owner, observer.IDs)
			done()
			if err != nil {
				record.RequestWarnings = append(record.RequestWarnings, "continuation ownership could not be saved")
			}
		}
		if copyErr != nil {
			record.Error = "upstream or downstream stream interrupted"
			if ctx.Err() != nil {
				record.StatusCode = 499
				record.Error = "client canceled"
			}
			if currentResponseStream(c) != nil {
				currentResponseStream(c).err = copyErr
				c.SetConnectionClose()
			} else {
				record.StatusCode = 502
				nativeError(c, 502, record.Error)
			}
			return
		}
		if response.StatusCode >= 400 {
			record.Error = fmt.Sprintf("upstream HTTP %d", response.StatusCode)
		} else {
			record.Error = ""
		}
		if !retry {
			return
		}
		s.appendRetryEvent(record, index, candidate.SourceID, record.Error)
	}
}

func nativeRetryStatus(status int) bool {
	switch status {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func addNativeUsage(record *usageRecord, u nativeusage.Usage) {
	add := func(target **int, value *int64) {
		if value == nil {
			return
		}
		if *target == nil {
			*target = new(int)
		}
		**target += int(*value)
	}
	add(&record.Usage.InputTokens, u.Input)
	add(&record.Usage.OutputTokens, u.Output)
	add(&record.Usage.TotalTokens, u.Total)
	add(&record.Usage.CacheHitTokens, u.CacheRead)
	add(&record.Usage.CacheCreationTokens, u.CacheWrite)
	add(&record.UsageDetail.ReasoningTokens, u.Reasoning)
	if u.Input != nil || u.Output != nil {
		record.UsageSource = "provider"
	} else {
		record.UsageSource = "missing"
	}
}
