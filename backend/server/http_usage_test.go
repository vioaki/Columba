package server

import (
	"context"
	"errors"
	"io"

	"net/http/httptest"
	"path/filepath"
	"strings"

	"testing"
	"time"

	"columba.local/backend/config"
	"columba.local/backend/logging"
	"columba.local/backend/storage"
	"github.com/bytedance/sonic"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func TestHTTPUsagePanicKeepsLogicalAndWireOutcomesDistinct(t *testing.T) {
	for _, isCommitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "committed"}[isCommitted], func(t *testing.T) {
			server, _ := newAdminTestServer(t)
			bodyLimit, errorsOnly := 1024, true
			server.config.SetUsageLogConfig(config.UsageLogConfig{BodyMaxKB: &bodyLimit, BodyOnErrorOnly: &errorsOnly})
			events := make(requestLogEvents, 8)
			server.logger = logging.New(logging.Options{Level: "debug", Format: "json"}, nil)
			server.logger.SetOutput(events)
			server.engine.Use(server.requestLifecycle(), server.requestLogging())
			server.engine.GET("/panic", func(ctx context.Context, c *app.RequestContext) {
				record := server.initUsageRecord(ctx, c, time.Now(), nil, "")
				installDownstreamCapture(c, record, bodyLimit<<10)
				defer server.recordHTTPUsage(c, record)
				if isCommitted {
					writer := startStream(c)
					_, _ = writer.Write([]byte("synthetic partial response"))
					_ = writer.Flush()
				} else {
					c.String(200, "synthetic uncommitted response")
				}
				panic("synthetic panic detail must not enter usage")
			})
			web := startTestServer(server.engine)
			defer web.Close()
			response, err := web.Client().Get(web.URL + "/panic")
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			for {
				select {
				case encoded := <-events:
					var event map[string]any
					if err := sonic.ConfigStd.Unmarshal(encoded, &event); err != nil {
						t.Fatal(err)
					}
					if event["message"] != "request completed" {
						continue
					}
				case <-time.After(3 * time.Second):
					t.Fatal("panic completion did not settle")
				}
				break
			}
			_, rows, err := server.store.QueryUsageLogs(t.Context(), storage.UsageQuery{})
			if err != nil || len(rows) != 1 {
				t.Fatal("panic settlement count", len(rows), err)
			}
			stored := storedRecordJSON(t, server.store, rows[0].RequestID)
			var record usageRecord
			if err := sonic.ConfigStd.Unmarshal([]byte(stored), &record); err != nil {
				t.Fatal(err)
			}
			if record.StatusCode != 500 || record.ErrorKind != "server_error" || strings.Contains(stored, "synthetic panic detail") || strings.Contains(stored, "goroutine ") {
				t.Fatal("unsafe or incorrect panic attribution", record.StatusCode, record.ErrorKind)
			}
			if isCommitted {
				if response.StatusCode != 200 || !errors.Is(readErr, io.ErrUnexpectedEOF) || record.Delivery.Outcome != deliveryFailed || record.DownstreamResponse.Content != "synthetic partial response" {
					t.Fatal("committed panic lost the wire boundary", record.Delivery, readErr)
				}
			} else if response.StatusCode != 500 || readErr != nil || string(body) != httpPanicResponse || record.Delivery.Outcome != deliveryCompleted || record.DownstreamResponse.Content != httpPanicResponse {
				t.Fatal("buffered panic was recorded as generation success or delivery failure", record.Delivery, record.DownstreamResponse)
			}
		})
	}
}

func TestHTTPUsageResetKeepsOriginalSettlementGeneration(t *testing.T) {
	server, _ := newAdminTestServer(t)
	request := newTestContext(httptest.NewRecorder())
	record := server.initUsageRecord(t.Context(), request, time.Now(), nil, "")
	server.recordHTTPUsage(request, record)
	reset := newTestContext(httptest.NewRecorder())
	server.resetUsage(t.Context(), reset)
	flushTestResponse(reset)
	finishHTTPRequest(request)
	if count, err := server.store.CountUsageRecords(t.Context()); err != nil || count != 0 {
		t.Fatal("completion resurrected a pre-reset record", count, err)
	}
	next := newTestContext(httptest.NewRecorder())
	server.recordHTTPUsage(next, server.initUsageRecord(t.Context(), next, time.Now(), nil, ""))
	finishHTTPRequest(next)
	if count, err := server.store.CountUsageRecords(t.Context()); err != nil || count != 1 {
		t.Fatal("post-reset settlement was dropped", count, err)
	}
}

func TestHTTPUsageReleasesCaptureAndKeepsPrimary(t *testing.T) {
	server, _ := newAdminTestServer(t)
	request := newTestContext(httptest.NewRecorder())
	record := server.initUsageRecord(t.Context(), request, time.Now(), nil, "")
	record.bodyOpts.maxBytes = 64
	capture := installDownstreamCapture(request, record, 2<<20)
	writer := startStream(request)
	_, _ = writer.Write([]byte(strings.Repeat("x", 2<<20)))
	record.Error, record.ErrorKind, record.StatusCode, record.UpstreamHTTPStatus = "synthetic provider failure", "rate_limit", 429, 429
	server.recordHTTPUsage(request, record)
	if record.downstream != nil || capture.buf != nil || capture.response != nil || currentResponseStream(request).capture != nil || record.assets.byHash != nil || record.assets.memo != nil {
		t.Fatal("pending completion retained capture-only state")
	}
	currentResponseStream(request).err = wrapDownstreamError(errors.New("synthetic finalization failure"), deliveryFinalize)
	finishHTTPRequest(request)
	if record.StatusCode != 429 || record.ErrorKind != "rate_limit" || record.Error != "synthetic provider failure" || record.Delivery.Outcome != deliveryFailed {
		t.Fatal("delivery replaced the provider's primary failure", record.StatusCode, record.ErrorKind)
	}
}

func TestHertzDeduplicatesOnlyReportedTypedDeliveryErrors(t *testing.T) {
	for _, handler := range []network.HandleSpecificError{&deliveryConn{&standard.Conn{}}, &deliveryTLSConn{&standard.TLSConn{}}} {
		failure := wrapDownstreamError(errors.New("synthetic socket failure"), deliveryFlush).(*downstreamWriteError)
		if handler.HandleSpecificError(failure, "") {
			t.Fatal("unreported error was hidden")
		}
		failure.isReported = true
		if !handler.HandleSpecificError(failure, "") {
			t.Fatal("reported error was emitted twice")
		}
		if handler.HandleSpecificError(errors.New(failure.Error()), "") {
			t.Fatal("an unrelated equal message was hidden")
		}
	}
	unknown := &testConnection{}
	if observeHTTPConnection(unknown) != unknown {
		t.Fatal("adapter changed an unknown transport")
	}
}

func TestHertzDeliveryLogHasOneOwner(t *testing.T) {
	for _, hasRequestLogger := range []bool{false, true} {
		t.Run(map[bool]string{false: "framework-only", true: "request-owner"}[hasRequestLogger], func(t *testing.T) {
			events := make(requestLogEvents, 32)
			logger := logging.New(logging.Options{Level: "debug", Format: "json"}, nil)
			logger.SetOutput(events)
			previous := logging.Default()
			logging.SetDefault(logger)
			t.Cleanup(func() { logging.SetDefault(previous) })
			server := &Server{logger: logger}
			engine := newTestEngine(t)
			if hasRequestLogger {
				engine.Use(server.requestLogging())
			}
			engine.GET("/stream", func(_ context.Context, c *app.RequestContext) {
				stream := startStream(c)
				_, _ = stream.Write([]byte("synthetic complete response"))
				_ = stream.Flush()
				owner := currentResponseStream(c)
				owner.ExtWriter = &finalizeFailureWriter{ExtWriter: owner.ExtWriter, err: errors.New("synthetic final chunk failure")}
			})
			web := startTestServer(engine)
			response, err := web.Client().Get(web.URL + "/stream")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			web.Close()
			requestLogs, frameworkLogs := 0, 0
			for len(events) > 0 {
				var event map[string]any
				if err := sonic.ConfigStd.Unmarshal(<-events, &event); err != nil {
					t.Fatal(err)
				}
				message, _ := event["message"].(string)
				if message == "request completed" {
					requestLogs++
				}
				if event["component"] == "hertz" && strings.Contains(message, "synthetic final chunk failure") {
					frameworkLogs++
				}
			}
			if hasRequestLogger && (requestLogs != 1 || frameworkLogs != 0) || !hasRequestLogger && (requestLogs != 0 || frameworkLogs != 1) {
				t.Fatal("delivery logging was duplicated or lost", requestLogs, frameworkLogs)
			}
		})
	}
}

func TestHTTPUsageFinalizationSettlesBeforeShutdownClosesWriter(t *testing.T) {
	server, configPath := newAdminTestServer(t)
	server.startUsageWriter()
	server.engine.Use(server.requestLifecycle(), server.requestLogging())
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server.engine.GET("/hold", func(ctx context.Context, c *app.RequestContext) {
		record := server.initUsageRecord(ctx, c, time.Now(), nil, "")
		defer server.recordHTTPUsage(c, record)
		writer := startStream(c)
		_, _ = writer.Write([]byte("synthetic response"))
		_ = writer.Flush()
		stream := currentResponseStream(c)
		stream.ExtWriter = &heldFinalizeWriter{ExtWriter: stream.ExtWriter, ctx: ctx, entered: entered, release: release}
	})
	web := startTestServer(server.engine)
	defer web.Close()
	defer close(release)
	response, err := web.Client().Get(web.URL + "/hold")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Finalize did not hold the response")
	}
	closed := make(chan struct{})
	go func() { server.doShutdown(); close(closed) }()
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	select {
	case <-closed:
	case <-time.After(8 * time.Second):
		t.Fatal("shutdown did not drain finalization")
	}
	if server.shutdownErr != nil {
		t.Fatal(server.shutdownErr)
	}
	reopened, err := storage.Open(filepath.Join(filepath.Dir(configPath), "test.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	_, records, err := reopened.QueryUsageLogs(t.Context(), storage.UsageQuery{})
	if err != nil || len(records) != 1 {
		t.Fatal("shutdown lost or repeated completion settlement", len(records), err)
	}
	var record usageRecord
	if err := sonic.ConfigStd.Unmarshal([]byte(storedRecordJSON(t, reopened, records[0].RequestID)), &record); err != nil {
		t.Fatal(err)
	}
	if record.StatusCode != 503 || record.ErrorKind != deliveryShutdown || record.Delivery == nil || record.Delivery.HTTPStatus != 200 || record.Delivery.Outcome != deliveryFailed {
		t.Fatal("shutdown was mislabeled or escaped the persistence barrier", record.StatusCode, record.ErrorKind, record.Delivery)
	}
}

type heldFinalizeWriter struct {
	network.ExtWriter
	ctx     context.Context
	entered chan<- struct{}
	release <-chan struct{}
}

func (w *heldFinalizeWriter) Finalize() error {
	w.entered <- struct{}{}
	select {
	case <-w.release:
		return w.ExtWriter.Finalize()
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}
