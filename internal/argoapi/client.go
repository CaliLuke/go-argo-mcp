package argoapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type Config struct {
	BaseURL            string
	Token              string
	Username           string
	Password           string
	InsecureSkipVerify bool
	TLSServerName      string
	RequestTimeout     time.Duration
	HTTPClient         *http.Client
}

type WorkflowSummary struct {
	Name       string
	Namespace  string
	Status     string
	Progress   string
	StartedAt  string
	FinishedAt string
}

type WorkflowDetail struct {
	Summary     WorkflowSummary
	Message     string
	Labels      map[string]string
	Annotations map[string]string
	Parameters  map[string]string
	Outputs     map[string]string
}

type WorkflowLogEntry struct {
	PodName string
	Content string
}

type CronWorkflowSummary struct {
	Name      string
	Namespace string
	Schedule  string
	Schedules []string
	Timezone  string
	Suspended bool
}

type CronWorkflowDetail struct {
	CronWorkflowSummary
	LastScheduledTime string
	NextScheduledTime string
}

type TemplateSummary struct {
	Name       string
	Namespace  string
	Entrypoint string
}

type TemplateDetail struct {
	TemplateSummary
	TemplateNames []string
}

type ClusterTemplateSummary struct {
	Name       string
	Entrypoint string
}

type ClusterTemplateDetail struct {
	ClusterTemplateSummary
	TemplateNames []string
}

type Client struct {
	baseURL  string
	http     *http.Client
	token    string
	username string
	password string
}

type HTTPError struct {
	StatusCode int
	Endpoint   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("argo returned status %d for %s", e.StatusCode, e.Endpoint)
}

func New(config Config) *Client {
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	baseClient := config.HTTPClient
	if baseClient == nil {
		baseClient = &http.Client{}
	}
	clientCopy := *baseClient
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}
	if transport, ok := clientCopy.Transport.(*http.Transport); ok {
		cloned := transport.Clone()
		if config.InsecureSkipVerify || config.TLSServerName != "" {
			tlsConfig := &tls.Config{
				InsecureSkipVerify: config.InsecureSkipVerify,
				ServerName:         config.TLSServerName,
			}
			if cloned.TLSClientConfig != nil {
				tlsConfig = cloned.TLSClientConfig.Clone()
				tlsConfig.InsecureSkipVerify = config.InsecureSkipVerify
				tlsConfig.ServerName = config.TLSServerName
			}
			cloned.TLSClientConfig = tlsConfig
		}
		clientCopy.Transport = cloned
	}
	clientCopy.Timeout = timeout
	return &Client{
		baseURL:  strings.TrimRight(config.BaseURL, "/"),
		http:     &clientCopy,
		token:    config.Token,
		username: config.Username,
		password: config.Password,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) ListWorkflows(ctx context.Context, namespace, status string, limit int) ([]WorkflowSummary, error) {
	return c.listWorkflows(ctx, namespace, status, limit, "")
}

func (c *Client) listWorkflows(ctx context.Context, namespace, status string, limit int, labelSelector string) ([]WorkflowSummary, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace)
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	workflows := make([]WorkflowSummary, 0)
	seenCursors := make(map[string]bool)
	for {
		pageSize := limit - len(workflows)
		if status != "" && pageSize < 100 {
			pageSize = 100
		}
		query["listOptions.limit"] = intString(pageSize)
		resp, err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil)
		if err != nil {
			return nil, err
		}
		for _, item := range jsonItems(resp) {
			summary := workflowSummaryFromObject(item)
			if summary.Name == "" || status != "" && !strings.EqualFold(summary.Status, status) {
				continue
			}
			workflows = append(workflows, summary)
			if limit > 0 && len(workflows) >= limit {
				return workflows, nil
			}
		}
		cursor := stringValue(objectValue(resp, "metadata"), "continue")
		if cursor == "" {
			return workflows, nil
		}
		if seenCursors[cursor] {
			return nil, fmt.Errorf("argo repeated workflow list continuation token")
		}
		seenCursors[cursor] = true
		query["listOptions.continue"] = cursor
	}
}

func (c *Client) GetWorkflow(ctx context.Context, namespace, name string) (*WorkflowDetail, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	summary := workflowSummaryFromObject(resp)
	if summary.Name == "" {
		return nil, fmt.Errorf("workflow metadata missing name for %s/%s", namespace, name)
	}
	status := objectValue(resp, "status")
	metadata := objectValue(resp, "metadata")
	return &WorkflowDetail{
		Summary:     summary,
		Message:     stringValue(status, "message"),
		Labels:      stringMap(metadata, "labels"),
		Annotations: stringMap(metadata, "annotations"),
		Parameters:  extractParameters(resp, "spec", "arguments"),
		Outputs:     extractParameters(resp, "status", "outputs"),
	}, nil
}

func (c *Client) GetWorkflowLogs(ctx context.Context, namespace, workflowName, podName, container string) ([]WorkflowLogEntry, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(workflowName) + "/log"
	query := map[string]string{"logOptions.container": container}
	if podName != "" {
		query["podName"] = podName
	}
	body, err := c.doText(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(body, "\n")
	entries := make([]WorkflowLogEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") ||
			strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, fmt.Errorf("decode Argo log stream frame: %w", err)
		}
		if _, failed := payload["error"]; failed {
			return nil, fmt.Errorf("argo log stream returned an error")
		}
		result := objectValue(payload, "result")
		if len(result) == 0 {
			result = payload
		}
		content := stringValue(result, "content")
		if content == "" {
			continue
		}
		entries = append(entries, WorkflowLogEntry{
			PodName: stringValue(result, "podName"),
			Content: content,
		})
	}
	return entries, nil
}

func (c *Client) RetryWorkflow(ctx context.Context, namespace, name string, restartSuccessful bool) error {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/retry"
	body := map[string]any{
		"name":              name,
		"namespace":         namespace,
		"restartSuccessful": restartSuccessful,
	}
	_, err := c.doJSON(ctx, http.MethodPut, endpoint, nil, body)
	return err
}

func (c *Client) TerminateWorkflow(ctx context.Context, namespace, name string) error {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/terminate"
	_, err := c.doJSON(ctx, http.MethodPut, endpoint, nil, map[string]any{
		"name":      name,
		"namespace": namespace,
	})
	return err
}

func (c *Client) ListCronWorkflows(ctx context.Context, namespace string, suspended *bool) ([]CronWorkflowSummary, error) {
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace)
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	items := jsonItems(resp)
	results := make([]CronWorkflowSummary, 0, len(items))
	for _, item := range items {
		summary := CronWorkflowSummary{
			Name:      stringValue(objectValue(item, "metadata"), "name"),
			Namespace: coalesce(stringValue(objectValue(item, "metadata"), "namespace"), namespace),
			Schedule:  cronSchedule(objectValue(item, "spec")),
			Schedules: cronSchedules(objectValue(item, "spec")),
			Timezone:  stringValue(objectValue(item, "spec"), "timezone"),
			Suspended: boolValue(objectValue(item, "spec"), "suspend"),
		}
		if summary.Name == "" {
			continue
		}
		if suspended != nil && summary.Suspended != *suspended {
			continue
		}
		results = append(results, summary)
	}
	return results, nil
}

func (c *Client) GetCronWorkflow(ctx context.Context, namespace, name string) (*CronWorkflowDetail, error) {
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	spec := objectValue(resp, "spec")
	status := objectValue(resp, "status")
	schedules := cronSchedules(spec)
	timezone := stringValue(spec, "timezone")
	suspended := boolValue(spec, "suspend")
	nextScheduledTime := ""
	if !suspended {
		nextScheduledTime = stringValue(status, "nextScheduledTime")
		if nextScheduledTime == "" {
			nextScheduledTime = nextCronRun(schedules, timezone, time.Now())
		}
	}
	return &CronWorkflowDetail{
		CronWorkflowSummary: CronWorkflowSummary{
			Name:      stringValue(objectValue(resp, "metadata"), "name"),
			Namespace: coalesce(stringValue(objectValue(resp, "metadata"), "namespace"), namespace),
			Schedule:  cronSchedule(spec),
			Schedules: schedules,
			Timezone:  timezone,
			Suspended: suspended,
		},
		LastScheduledTime: stringValue(status, "lastScheduledTime"),
		NextScheduledTime: nextScheduledTime,
	}, nil
}

func (c *Client) ToggleCronSuspension(ctx context.Context, namespace, name string, suspend bool) error {
	action := "resume"
	if suspend {
		action = "suspend"
	}
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/" + action
	_, err := c.doJSON(ctx, http.MethodPut, endpoint, nil, map[string]any{
		"name":      name,
		"namespace": namespace,
	})
	return err
}

func (c *Client) GetCronHistory(ctx context.Context, namespace, name string, limit int) ([]WorkflowSummary, error) {
	if _, err := c.GetCronWorkflow(ctx, namespace, name); err != nil {
		return nil, err
	}
	labelSelector := "workflows.argoproj.io/cron-workflow=" + name
	history, err := c.listWorkflows(ctx, namespace, "", limit, labelSelector)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(history, func(i, j int) bool {
		return history[i].StartedAt > history[j].StartedAt
	})
	return history, nil
}

func (c *Client) ListWorkflowTemplates(ctx context.Context, namespace, labelSelector string) ([]TemplateSummary, error) {
	endpoint := c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace)
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, err
	}
	items := jsonItems(resp)
	results := make([]TemplateSummary, 0, len(items))
	for _, item := range items {
		results = append(results, templateSummaryFromObject(item, namespace))
	}
	return compactTemplateSummaries(results), nil
}

func (c *Client) GetWorkflowTemplate(ctx context.Context, namespace, name string) (*TemplateDetail, error) {
	endpoint := c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	summary := templateSummaryFromObject(resp, namespace)
	return &TemplateDetail{
		TemplateSummary: summary,
		TemplateNames:   templateNames(resp),
	}, nil
}

func (c *Client) ListClusterWorkflowTemplates(ctx context.Context, labelSelector string) ([]ClusterTemplateSummary, error) {
	endpoint := c.baseURL + "/api/v1/cluster-workflow-templates"
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, err
	}
	items := jsonItems(resp)
	results := make([]ClusterTemplateSummary, 0, len(items))
	for _, item := range items {
		name := stringValue(objectValue(item, "metadata"), "name")
		if name == "" {
			continue
		}
		results = append(results, ClusterTemplateSummary{
			Name:       name,
			Entrypoint: stringValue(objectValue(item, "spec"), "entrypoint"),
		})
	}
	return results, nil
}

func (c *Client) GetClusterWorkflowTemplate(ctx context.Context, name string) (*ClusterTemplateDetail, error) {
	endpoint := c.baseURL + "/api/v1/cluster-workflow-templates/" + url.PathEscape(name)
	resp, err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ClusterTemplateDetail{
		ClusterTemplateSummary: ClusterTemplateSummary{
			Name:       stringValue(objectValue(resp, "metadata"), "name"),
			Entrypoint: stringValue(objectValue(resp, "spec"), "entrypoint"),
		},
		TemplateNames: templateNames(resp),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, query map[string]string, body any) (map[string]any, error) {
	req, err := c.newRequest(ctx, method, endpoint, query, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode json from %s: %w", endpoint, err)
	}
	return payload, nil
}

func (c *Client) doText(ctx context.Context, method, endpoint string, query map[string]string, body any) (string, error) {
	req, err := c.newRequest(ctx, method, endpoint, query, body)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("read body from %s: %w", endpoint, err)
	}
	return buf.String(), nil
}

func (c *Client) newRequest(ctx context.Context, method, endpoint string, query map[string]string, body any) (*http.Request, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("argo client is not configured")
	}
	if len(query) > 0 {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse url %s: %w", endpoint, err)
		}
		values := u.Query()
		for key, value := range query {
			if value != "" {
				values.Set(key, value)
			}
		}
		u.RawQuery = values.Encode()
		endpoint = u.String()
	}

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	return req, nil
}

func workflowSummaryFromObject(item map[string]any) WorkflowSummary {
	metadata := objectValue(item, "metadata")
	status := objectValue(item, "status")
	return WorkflowSummary{
		Name:       stringValue(metadata, "name"),
		Namespace:  stringValue(metadata, "namespace"),
		Status:     coalesce(stringValue(status, "phase"), "Unknown"),
		Progress:   stringValue(status, "progress"),
		StartedAt:  stringValue(status, "startedAt"),
		FinishedAt: stringValue(status, "finishedAt"),
	}
}

func templateSummaryFromObject(item map[string]any, namespace string) TemplateSummary {
	metadata := objectValue(item, "metadata")
	spec := objectValue(item, "spec")
	return TemplateSummary{
		Name:       stringValue(metadata, "name"),
		Namespace:  coalesce(stringValue(metadata, "namespace"), namespace),
		Entrypoint: stringValue(spec, "entrypoint"),
	}
}

func compactTemplateSummaries(input []TemplateSummary) []TemplateSummary {
	out := make([]TemplateSummary, 0, len(input))
	for _, item := range input {
		if item.Name != "" {
			out = append(out, item)
		}
	}
	return out
}

func templateNames(item map[string]any) []string {
	spec := objectValue(item, "spec")
	raw := anySlice(spec["templates"])
	names := make([]string, 0, len(raw))
	for _, entry := range raw {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if name := stringValue(obj, "name"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cronSchedules(spec map[string]any) []string {
	raw := anySlice(spec["schedules"])
	schedules := make([]string, 0, len(raw))
	for _, value := range raw {
		if schedule, ok := value.(string); ok && schedule != "" {
			schedules = append(schedules, schedule)
		}
	}
	if len(schedules) == 0 {
		if schedule := stringValue(spec, "schedule"); schedule != "" {
			return []string{schedule}
		}
	}
	return schedules
}

func cronSchedule(spec map[string]any) string {
	if schedules := cronSchedules(spec); len(schedules) > 0 {
		return schedules[0]
	}
	return ""
}

func nextCronRun(schedules []string, timezone string, now time.Time) string {
	var next time.Time
	for _, expression := range schedules {
		if timezone != "" {
			expression = "CRON_TZ=" + timezone + " " + expression
		}
		schedule, err := cron.ParseStandard(expression)
		if err != nil {
			continue
		}
		candidate := schedule.Next(now)
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	if next.IsZero() {
		return ""
	}
	return next.UTC().Format(time.RFC3339)
}

func extractParameters(item map[string]any, parentKey, argumentsKey string) map[string]string {
	parent := objectValue(item, parentKey)
	args := objectValue(parent, argumentsKey)
	params := anySlice(args["parameters"])
	out := make(map[string]string, len(params))
	for _, entry := range params {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(obj, "name")
		if name == "" {
			continue
		}
		out[name] = coalesce(
			stringValue(obj, "value"),
			stringValue(obj, "default"),
			stringJSONValue(obj["valueFrom"]),
		)
	}
	return out
}

func stringMap(item map[string]any, key string) map[string]string {
	raw := objectValue(item, key)
	if len(raw) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = stringJSONValue(v)
	}
	return out
}

func objectValue(item map[string]any, key string) map[string]any {
	if item == nil {
		return map[string]any{}
	}
	raw, ok := item[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return raw
}

func jsonItems(item map[string]any) []map[string]any {
	raw := anySlice(item["items"])
	out := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		obj, ok := entry.(map[string]any)
		if ok {
			out = append(out, obj)
		}
	}
	return out
}

func anySlice(v any) []any {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	return raw
}

func stringValue(item map[string]any, key string) string {
	value, ok := item[key]
	if !ok {
		return ""
	}
	return stringJSONValue(value)
}

func boolValue(item map[string]any, key string) bool {
	value, ok := item[key].(bool)
	return ok && value
}

func stringJSONValue(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	case float64:
		if value == float64(int64(value)) {
			return fmt.Sprintf("%d", int64(value))
		}
		return fmt.Sprintf("%v", value)
	case map[string]any, []any:
		data, _ := json.Marshal(value)
		return string(data)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func intString(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", v)
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
