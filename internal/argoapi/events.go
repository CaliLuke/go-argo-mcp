package argoapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CaliLuke/go-argo-mcp/internal/argoapi/models"
)

const maxEventStreamBytes = 1 << 20
const maxEventFrameBytes = 64 << 10
const eventReaderShutdownTimeout = time.Second

type WorkflowEvent struct {
	Type, Reason, Message                    string
	Count                                    int
	FirstTimestamp, LastTimestamp, EventTime string
}

type EventObservation struct {
	Events       []WorkflowEvent
	LimitReached bool
}

type eventFrame struct {
	event *WorkflowEvent
	err   error
}

type eventGatewayField struct {
	value   json.RawMessage
	present bool
}

func (f *eventGatewayField) UnmarshalJSON(data []byte) error {
	f.present = true
	f.value = append(f.value[:0], data...)
	return nil
}

type eventGatewayEnvelope struct {
	Result eventGatewayField `json:"result"`
	Error  eventGatewayField `json:"error"`
}

func (c *Client) ObserveWorkflowEvents(ctx context.Context, namespace, name string, limit int, duration time.Duration) (out EventObservation, resultErr error) {
	if limit < 1 || limit > 200 {
		return EventObservation{}, fmt.Errorf("event limit must be between 1 and 200")
	}
	if duration < time.Second || duration > 10*time.Second {
		return EventObservation{}, fmt.Errorf("event duration must be between 1 and 10 seconds")
	}
	wf, err := c.GetWorkflowData(ctx, namespace, name)
	if err != nil {
		return EventObservation{}, err
	}
	if wf.UID == "" {
		return EventObservation{}, fmt.Errorf("workflow UID is missing")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	timer := time.NewTimer(duration)
	defer timer.Stop()
	frames := make(chan eventFrame)
	bodies := make(chan io.ReadCloser)
	readerDone := make(chan struct{})
	go c.readEventWatch(watchCtx, namespace, name, wf.UID, duration, bodies, frames, readerDone)
	var body io.ReadCloser
	defer func() {
		cancel()
		if body == nil {
			return
		}
		_ = body.Close()
		shutdownTimer := time.NewTimer(eventReaderShutdownTimeout)
		defer shutdownTimer.Stop()
		select {
		case <-readerDone:
		case <-shutdownTimer.C:
			if resultErr == nil {
				resultErr = fmt.Errorf("event stream reader did not stop after body close")
			} else {
				resultErr = fmt.Errorf("%w: event stream reader did not stop after body close", resultErr)
			}
		}
	}()
	out = EventObservation{Events: make([]WorkflowEvent, 0, limit)}
	bodyEvents := (<-chan io.ReadCloser)(bodies)
	for {
		select {
		case <-ctx.Done():
			return EventObservation{}, ctx.Err()
		case <-timer.C:
			if err := ctx.Err(); err != nil {
				return EventObservation{}, err
			}
			return out, nil
		case acquiredBody, ok := <-bodyEvents:
			if !ok {
				bodyEvents = nil
				continue
			}
			body = acquiredBody
			bodyEvents = nil
		case frame, ok := <-frames:
			if !ok {
				if err := ctx.Err(); err != nil {
					return EventObservation{}, err
				}
				return out, nil
			}
			if frame.err != nil {
				return EventObservation{}, frame.err
			}
			out.Events = append(out.Events, *frame.event)
			if len(out.Events) == limit {
				out.LimitReached = true
				if err := ctx.Err(); err != nil {
					return EventObservation{}, err
				}
				return out, nil
			}
		}
	}
}

func (c *Client) readEventWatch(ctx context.Context, namespace, name, uid string, duration time.Duration, bodies chan<- io.ReadCloser, frames chan<- eventFrame, done chan<- struct{}) {
	defer close(done)
	defer close(bodies)
	defer close(frames)
	endpoint := c.baseURL + "/api/v1/stream/events/" + url.PathEscape(namespace)
	selector := "involvedObject.uid=" + uid + ",involvedObject.kind=Workflow,involvedObject.name=" + name
	query := map[string]string{"listOptions.fieldSelector": selector, "listOptions.timeoutSeconds": strconv.Itoa(int(duration/time.Second) + 5)}
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		sendEventError(ctx, frames, err)
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			sendEventError(ctx, frames, fmt.Errorf("GET %s: %w", endpoint, err))
		}
		return
	}
	select {
	case bodies <- resp.Body:
	case <-ctx.Done():
		_ = resp.Body.Close()
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		sendEventError(ctx, frames, &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint})
		return
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxEventStreamBytes+1))
	scanner.Buffer(make([]byte, 4096), maxEventFrameBytes)
	total := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		total += len(line) + 1
		if total > maxEventStreamBytes {
			sendEventError(ctx, frames, fmt.Errorf("event stream exceeds 1 MiB"))
			return
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == ':' || bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte("id:")) || bytes.HasPrefix(trimmed, []byte("retry:")) {
			continue
		}
		trimmed = bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
		event, err := decodeEventFrame(trimmed)
		if err != nil {
			sendEventError(ctx, frames, err)
			return
		}
		select {
		case frames <- eventFrame{event: &event}:
		case <-ctx.Done():
			return
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		if strings.Contains(err.Error(), "token too long") {
			err = fmt.Errorf("event frame exceeds 64 KiB")
		}
		sendEventError(ctx, frames, err)
	}
}

func sendEventError(ctx context.Context, frames chan<- eventFrame, err error) {
	select {
	case frames <- eventFrame{err: err}:
	case <-ctx.Done():
	}
}

func decodeEventFrame(data []byte) (WorkflowEvent, error) {
	var envelope eventGatewayEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return WorkflowEvent{}, fmt.Errorf("decode event stream frame: %w", err)
	}
	if envelope.Error.present {
		return WorkflowEvent{}, fmt.Errorf("argo event stream returned an error")
	}
	if !envelope.Result.present || bytes.Equal(bytes.TrimSpace(envelope.Result.value), []byte("null")) {
		return WorkflowEvent{}, fmt.Errorf("argo event stream frame has no result")
	}
	var payload models.Event
	if err := json.Unmarshal(envelope.Result.value, &payload); err != nil {
		return WorkflowEvent{}, fmt.Errorf("decode event result: %w", err)
	}
	return WorkflowEvent{
		Type:           payload.Type,
		Reason:         payload.Reason,
		Message:        payload.Message,
		Count:          payload.Count,
		FirstTimestamp: payload.FirstTimestamp,
		LastTimestamp:  payload.LastTimestamp,
		EventTime:      payload.EventTime,
	}, nil
}
