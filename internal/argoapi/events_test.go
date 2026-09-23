package argoapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type eventRoundTripFunc func(*http.Request) (*http.Response, error)

func (f eventRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type closeBlockedBody struct {
	readStarted chan struct{}
	closed      chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
}

func newCloseBlockedBody() *closeBlockedBody {
	return &closeBlockedBody{readStarted: make(chan struct{}), closed: make(chan struct{})}
}

func (b *closeBlockedBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.readStarted) })
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *closeBlockedBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func requireEventSignal(t *testing.T, signal <-chan struct{}, description string, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func TestObserveWorkflowEventsScopesWatchAndStopsAtLimit(t *testing.T) {
	watchClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/workflows/argo-ci/build" {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		if r.URL.Path != "/api/v1/stream/events/argo-ci" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		selector := r.URL.Query().Get("listOptions.fieldSelector")
		for _, part := range []string{"involvedObject.uid=u-1", "involvedObject.kind=Workflow", "involvedObject.name=build"} {
			if !strings.Contains(selector, part) {
				t.Fatalf("selector=%q", selector)
			}
		}
		if r.URL.Query().Get("listOptions.timeoutSeconds") != "6" {
			t.Fatalf("timeout=%q", r.URL.Query().Get("listOptions.timeoutSeconds"))
		}
		_, _ = w.Write([]byte(": heartbeat\n\ndata: {\"result\":{\"type\":\"Warning\",\"reason\":\"Failed\",\"message\":\"boom\",\"count\":2,\"firstTimestamp\":\"2026-09-22T01:00:00Z\",\"lastTimestamp\":\"2026-09-22T01:01:00Z\",\"eventTime\":\"2026-09-22T01:00:00.123456Z\"}}\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(watchClosed)
	}))
	defer server.Close()
	got, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 1, time.Second)
	if err != nil || !got.LimitReached || len(got.Events) != 1 || got.Events[0].Reason != "Failed" || got.Events[0].Count != 2 || got.Events[0].EventTime != "2026-09-22T01:00:00.123456Z" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	requireEventSignal(t, watchClosed, "limited watch cancellation", time.Second)
}

func TestObserveWorkflowEventsDeadlineSucceedsBeforeHeadersAndCloses(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	start := time.Now()
	got, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second)
	if err != nil || len(got.Events) != 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("got=%#v elapsed=%v err=%v", got, time.Since(start), err)
	}
	requireEventSignal(t, closed, "pre-header watch cancellation", time.Second)
}

func TestObserveWorkflowEventsRejectsMalformedFramesAndMissingUID(t *testing.T) {
	for _, frame := range []string{`{}`, `{"result":null}`, `{"error":null,"result":{"type":"Normal"}}`, `{"result":{"count":"2"}}`} {
		t.Run(frame, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/workflows/") {
					_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
					return
				}
				_, _ = w.Write([]byte(frame + "\n"))
			}))
			defer server.Close()
			if _, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second); err == nil {
				t.Fatal("malformed/error frame accepted")
			}
		})
	}
	var watchCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stream/") {
			watchCalls.Add(1)
		}
		_, _ = w.Write([]byte(`{"metadata":{"name":"build"}}`))
	}))
	defer server.Close()
	if _, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second); err == nil || watchCalls.Load() != 0 {
		t.Fatalf("err=%v watchCalls=%d", err, watchCalls.Load())
	}
}

func TestObserveWorkflowEventsParentCancellationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(ctx, "argo-ci", "build", 5, time.Second); err == nil {
		t.Fatal("parent cancellation succeeded")
	}
}

func TestObserveWorkflowEventsParentCancellationClosesAcquiredBlockedBody(t *testing.T) {
	body := newCloseBlockedBody()
	t.Cleanup(func() { _ = body.Close() })
	client := New(Config{BaseURL: "https://argo.example", HTTPClient: &http.Client{Transport: eventRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		responseBody := io.NopCloser(strings.NewReader(`{"metadata":{"name":"build","uid":"u-1"}}`))
		if strings.Contains(r.URL.Path, "/stream/") {
			responseBody = body
		}
		return &http.Response{StatusCode: http.StatusOK, Body: responseBody, Header: make(http.Header), Request: r}, nil
	})}})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.ObserveWorkflowEvents(ctx, "argo-ci", "build", 5, 5*time.Second)
		result <- err
	}()
	requireEventSignal(t, body.readStarted, "blocked event read", time.Second)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("observation did not return after parent cancellation")
	}
	requireEventSignal(t, body.closed, "event body close", time.Second)
}

func TestObserveWorkflowEventsClosesRealHTTPWatchOnParentCancellation(t *testing.T) {
	headersFlushed := make(chan struct{})
	watchClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(headersFlushed)
		<-r.Context().Done()
		close(watchClosed)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(ctx, "argo-ci", "build", 5, 5*time.Second)
		result <- err
	}()
	requireEventSignal(t, headersFlushed, "watch headers", time.Second)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("observation did not return after parent cancellation")
	}
	requireEventSignal(t, watchClosed, "server watch cancellation", time.Second)
}

func TestObserveWorkflowEventsOwnWindowClosesRealHTTPWatchAndSucceeds(t *testing.T) {
	headersFlushed := make(chan struct{})
	watchClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(headersFlushed)
		<-r.Context().Done()
		close(watchClosed)
	}))
	defer server.Close()
	result := make(chan struct {
		observation EventObservation
		err         error
	}, 1)
	go func() {
		observation, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second)
		result <- struct {
			observation EventObservation
			err         error
		}{observation: observation, err: err}
	}()
	requireEventSignal(t, headersFlushed, "watch headers", time.Second)
	select {
	case got := <-result:
		if got.err != nil || len(got.observation.Events) != 0 {
			t.Fatalf("got=%#v err=%v", got.observation, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("observation did not return after its window expired")
	}
	requireEventSignal(t, watchClosed, "server watch cancellation", time.Second)
}

func TestObserveWorkflowEventsEOFClosesBodyAndSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
			return
		}
		_, _ = w.Write([]byte("{\"result\":{\"type\":\"Normal\",\"reason\":\"Completed\"}}\n"))
	}))
	defer server.Close()
	got, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second)
	if err != nil || len(got.Events) != 1 || got.Events[0].Reason != "Completed" || got.LimitReached {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestObserveWorkflowEventsRejectsFrameAndStreamOverflow(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"frame", strings.Repeat("x", maxEventFrameBytes+1) + "\n"},
		{"stream", strings.Repeat(": "+strings.Repeat("x", 60000)+"\n", 18)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/workflows/") {
					_, _ = w.Write([]byte(`{"metadata":{"name":"build","uid":"u-1"}}`))
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if _, err := New(Config{BaseURL: server.URL}).ObserveWorkflowEvents(context.Background(), "argo-ci", "build", 5, time.Second); err == nil {
				t.Fatal("overflow accepted")
			}
		})
	}
}
