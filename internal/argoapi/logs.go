package argoapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

func (c *Client) GetWorkflowLogs(ctx context.Context, namespace, workflowName, podName, container string, maxBytes int) ([]WorkflowLogEntry, bool, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(workflowName) + "/log"
	query := map[string]string{"logOptions.container": container}
	if podName != "" {
		query["podName"] = podName
	}
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("GET workflow logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
	}
	encoding := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, false, fmt.Errorf("argo log response uses unsupported content encoding")
	}
	if maxBytes <= 0 {
		return nil, false, fmt.Errorf("max_bytes must be positive")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, false, fmt.Errorf("read Argo log stream: %w", err)
	}
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
		if last := strings.LastIndexByte(string(body), '\n'); last >= 0 {
			body = body[:last+1]
		} else {
			body = nil
		}
	}
	entries, err := decodeLogFrames(string(body))
	if err != nil {
		return nil, truncated, err
	}
	if truncated && len(entries) == 0 {
		return nil, true, fmt.Errorf("no complete log frame fit within max_bytes; increase max_bytes")
	}
	return entries, truncated, nil
}

func decodeLogFrames(body string) ([]WorkflowLogEntry, error) {
	if !utf8.ValidString(body) {
		return nil, fmt.Errorf("argo log stream is not valid UTF-8")
	}
	lines := strings.Split(body, "\n")
	entries := make([]WorkflowLogEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var payload logEnvelope
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, fmt.Errorf("decode Argo log stream frame: %w", err)
		}
		if len(payload.Error) != 0 {
			return nil, fmt.Errorf("argo log stream returned an error")
		}
		result := logResult{}
		if !emptyJSONValue(payload.Result) {
			if err := json.Unmarshal(payload.Result, &result); err != nil {
				return nil, fmt.Errorf("decode Argo log stream result: %w", err)
			}
		} else {
			result = logResult{PodName: payload.PodName, Content: payload.Content}
		}
		if result.Content != "" {
			entries = append(entries, WorkflowLogEntry(result))
		}
	}
	return entries, nil
}
