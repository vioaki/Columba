package server

import (
	"context"
	"errors"
	"io"
	"net"

	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/cloudwego/hertz/pkg/network"
)

type finalizeFailureWriter struct {
	network.ExtWriter
	err   error
	calls int
}

func (writer *finalizeFailureWriter) Finalize() error {
	writer.calls++
	return writer.err
}

func TestResponseStreamRemembersFinalizeFailure(t *testing.T) {
	cause := errors.New("synthetic final chunk failure")
	native := &finalizeFailureWriter{err: cause}
	stream := &responseStream{ExtWriter: native}
	for range 2 {
		if err := stream.Finalize(); !errors.Is(err, cause) {
			t.Fatal("finalization lost the original cause", err)
		}
	}
	if native.calls != 1 || !errors.Is(stream.err, cause) {
		t.Fatal("finalization was repeated or its failure was not retained", native.calls, stream.err)
	}
}

func TestHTTPCompletionFreezesBeforeCleanupCancellation(t *testing.T) {
	for _, cancelBefore := range []bool{false, true} {
		ctx, cancel := context.WithCancelCause(t.Context())
		request := newTestContext(httptest.NewRecorder())
		state := trackHTTPDelivery(ctx, request)
		if cancelBefore {
			cancel(errHTTPServerShutdown)
		}
		onHTTPComplete(request, func() { cancel(errHTTPServerShutdown) })
		finishHTTPRequest(request)
		if state.result.Outcome != deliveryCompleted || state.err != nil || state.ctx != nil {
			t.Fatal("cleanup or a late cancellation changed successful transport", state)
		}
		if (state.result.Cause == deliveryShutdown) != cancelBefore {
			t.Fatal("completion did not preserve the observed cancellation boundary", state.result)
		}
	}
}

func TestHTTPDeliveryLookupPreservesTrackedCause(t *testing.T) {
	request := newTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errHTTPServerShutdown)
	state := trackHTTPDelivery(ctx, request)
	if ensureHTTPDeliveryState(request) != state || state.ctx != ctx {
		t.Fatal("state lookup replaced its cancellation context")
	}
	freezeHTTPDelivery(request)
	trackHTTPDelivery(context.Background(), request)
	if state.ctx != nil || state.result.Cause != deliveryShutdown {
		t.Fatal("tracking after completion changed frozen evidence")
	}
}

func TestDownstreamClosureWithoutPeerEvidenceRemainsUnknown(t *testing.T) {
	for _, cause := range []error{io.EOF, io.ErrClosedPipe, syscall.EPIPE, net.ErrClosed} {
		record := &usageRecord{StatusCode: 200}
		record.StatusCode, record.ErrorKind = deliveryFailureStatus(deliveryFailureCause(nil, wrapDownstreamError(cause, deliveryFlush)))
		if record.StatusCode != 502 || record.ErrorKind != deliveryTransport {
			t.Fatal("an ambiguous closure became a peer disconnect", cause, record.StatusCode, record.ErrorKind)
		}
	}
}

func TestDeliveryPanicPrecedesConcurrentCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, errHTTPServerShutdown, errDownstreamDisconnected} {
		kind := deliveryFailureCause(cause, wrapDownstreamError(errResponseAborted, deliveryAbort))
		if status, _ := deliveryFailureStatus(kind); status != 500 || kind != "server_panic" {
			t.Fatal("cancellation replaced a known panic", status, kind)
		}
	}
}
