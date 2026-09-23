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
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/CaliLuke/go-argo-mcp/internal/argoapi/models"
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

type Page[T any] struct {
	Items    []T
	Continue string
}

const (
	defaultCollectionLimit = 50
	defaultHistoryLimit    = 10
	maximumCollectionLimit = 200
	maximumBackendPages    = 1000
)

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

func (c *Client) ListWorkflows(ctx context.Context, namespace, status string, limit int, continueToken string) (Page[WorkflowSummary], error) {
	return c.listWorkflows(ctx, namespace, status, limit, continueToken, "", defaultCollectionLimit)
}

func (c *Client) listWorkflows(ctx context.Context, namespace, status string, limit int, continueToken, labelSelector string, defaultLimit int) (Page[WorkflowSummary], error) {
	limit, err := boundedLimit(limit, defaultLimit)
	if err != nil {
		return Page[WorkflowSummary]{}, err
	}
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace)
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	return scanPages(ctx, limit, continueToken, "workflow list", func(ctx context.Context, pageSize int, cursor string) ([]models.Workflow, string, error) {
		var resp models.WorkflowList
		return fetchListPage(ctx, c, endpoint, query, pageSize, cursor, &resp, func() ([]models.Workflow, string) {
			return resp.Items, resp.Metadata.Continue
		})
	}, func(item models.Workflow) (WorkflowSummary, bool) {
		summary := workflowSummaryFromModel(item)
		if summary.Name == "" || status != "" && !strings.EqualFold(summary.Status, status) {
			return WorkflowSummary{}, false
		}
		return summary, true
	})
}

func (c *Client) GetWorkflow(ctx context.Context, namespace, name string) (*WorkflowDetail, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	var resp models.Workflow
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	summary := workflowSummaryFromModel(resp)
	if summary.Name == "" {
		return nil, fmt.Errorf("workflow metadata missing name for %s/%s", namespace, name)
	}
	return &WorkflowDetail{
		Summary:     summary,
		Message:     resp.Status.Message,
		Labels:      nonnilStringMap(resp.Metadata.Labels),
		Annotations: nonnilStringMap(resp.Metadata.Annotations),
		Parameters:  renderParameters(resp.Spec.Arguments.Parameters),
		Outputs:     renderParameters(resp.Status.Outputs.Parameters),
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
		if result.Content == "" {
			continue
		}
		entries = append(entries, WorkflowLogEntry(result))
	}
	return entries, nil
}

func (c *Client) RetryWorkflow(ctx context.Context, namespace, name string, restartSuccessful bool) error {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/retry"
	body := models.WorkflowRetryRequest{
		Name:              name,
		Namespace:         namespace,
		RestartSuccessful: restartSuccessful,
	}
	return c.doJSON(ctx, http.MethodPut, endpoint, nil, body, nil)
}

func (c *Client) TerminateWorkflow(ctx context.Context, namespace, name string) error {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/terminate"
	body := models.WorkflowTerminateRequest{Name: name, Namespace: namespace}
	return c.doJSON(ctx, http.MethodPut, endpoint, nil, body, nil)
}

func (c *Client) ListCronWorkflows(ctx context.Context, namespace string, suspended *bool, limit int, continueToken string) (Page[CronWorkflowSummary], error) {
	limit, err := boundedLimit(limit, defaultCollectionLimit)
	if err != nil {
		return Page[CronWorkflowSummary]{}, err
	}
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace)
	query := map[string]string{}
	return scanPages(ctx, limit, continueToken, "CronWorkflow list", func(ctx context.Context, pageSize int, cursor string) ([]models.CronWorkflow, string, error) {
		var resp models.CronWorkflowList
		return fetchListPage(ctx, c, endpoint, query, pageSize, cursor, &resp, func() ([]models.CronWorkflow, string) {
			return resp.Items, resp.Metadata.Continue
		})
	}, func(item models.CronWorkflow) (CronWorkflowSummary, bool) {
		summary := CronWorkflowSummary{
			Name:      item.Metadata.Name,
			Namespace: coalesce(item.Metadata.Namespace, namespace),
			Schedule:  cronSchedule(item.Spec),
			Schedules: cronSchedules(item.Spec),
			Timezone:  item.Spec.Timezone,
			Suspended: item.Spec.Suspend,
		}
		if summary.Name == "" {
			return CronWorkflowSummary{}, false
		}
		if suspended != nil && summary.Suspended != *suspended {
			return CronWorkflowSummary{}, false
		}
		return summary, true
	})
}

func (c *Client) GetCronWorkflow(ctx context.Context, namespace, name string) (*CronWorkflowDetail, error) {
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	var resp models.CronWorkflow
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	schedules := cronSchedules(resp.Spec)
	timezone := resp.Spec.Timezone
	suspended := resp.Spec.Suspend
	nextScheduledTime := ""
	if !suspended {
		nextScheduledTime = resp.Status.NextScheduledTime
		if nextScheduledTime == "" {
			nextScheduledTime = nextCronRun(schedules, timezone, time.Now())
		}
	}
	return &CronWorkflowDetail{
		CronWorkflowSummary: CronWorkflowSummary{
			Name:      resp.Metadata.Name,
			Namespace: coalesce(resp.Metadata.Namespace, namespace),
			Schedule:  cronSchedule(resp.Spec),
			Schedules: schedules,
			Timezone:  timezone,
			Suspended: suspended,
		},
		LastScheduledTime: resp.Status.LastScheduledTime,
		NextScheduledTime: nextScheduledTime,
	}, nil
}

func (c *Client) ToggleCronSuspension(ctx context.Context, namespace, name string, suspend bool) error {
	action := "resume"
	if suspend {
		action = "suspend"
	}
	endpoint := c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/" + action
	if suspend {
		body := models.CronWorkflowSuspendRequest{Name: name, Namespace: namespace}
		return c.doJSON(ctx, http.MethodPut, endpoint, nil, body, nil)
	}
	body := models.CronWorkflowResumeRequest{Name: name, Namespace: namespace}
	return c.doJSON(ctx, http.MethodPut, endpoint, nil, body, nil)
}

func (c *Client) GetCronHistory(ctx context.Context, namespace, name string, limit int, continueToken string) (Page[WorkflowSummary], error) {
	if _, err := boundedLimit(limit, defaultHistoryLimit); err != nil {
		return Page[WorkflowSummary]{}, err
	}
	if _, err := c.GetCronWorkflow(ctx, namespace, name); err != nil {
		return Page[WorkflowSummary]{}, err
	}
	labelSelector := "workflows.argoproj.io/cron-workflow=" + name
	history, err := c.listWorkflows(ctx, namespace, "", limit, continueToken, labelSelector, defaultHistoryLimit)
	if err != nil {
		return Page[WorkflowSummary]{}, err
	}
	sort.SliceStable(history.Items, func(i, j int) bool {
		return history.Items[i].StartedAt > history.Items[j].StartedAt
	})
	return history, nil
}

func (c *Client) ListWorkflowTemplates(ctx context.Context, namespace, labelSelector string, limit int, continueToken string) (Page[TemplateSummary], error) {
	limit, err := boundedLimit(limit, defaultCollectionLimit)
	if err != nil {
		return Page[TemplateSummary]{}, err
	}
	endpoint := c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace)
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	return scanPages(ctx, limit, continueToken, "WorkflowTemplate list", func(ctx context.Context, pageSize int, cursor string) ([]models.WorkflowTemplate, string, error) {
		var resp models.WorkflowTemplateList
		return fetchListPage(ctx, c, endpoint, query, pageSize, cursor, &resp, func() ([]models.WorkflowTemplate, string) {
			return resp.Items, resp.Metadata.Continue
		})
	}, func(item models.WorkflowTemplate) (TemplateSummary, bool) {
		summary := templateSummaryFromModel(item.Metadata, item.Spec, namespace)
		return summary, summary.Name != ""
	})
}

func (c *Client) GetWorkflowTemplate(ctx context.Context, namespace, name string) (*TemplateDetail, error) {
	endpoint := c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	var resp models.WorkflowTemplate
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	summary := templateSummaryFromModel(resp.Metadata, resp.Spec, namespace)
	return &TemplateDetail{
		TemplateSummary: summary,
		TemplateNames:   templateNames(resp.Spec),
	}, nil
}

func (c *Client) ListClusterWorkflowTemplates(ctx context.Context, labelSelector string, limit int, continueToken string) (Page[ClusterTemplateSummary], error) {
	limit, err := boundedLimit(limit, defaultCollectionLimit)
	if err != nil {
		return Page[ClusterTemplateSummary]{}, err
	}
	endpoint := c.baseURL + "/api/v1/cluster-workflow-templates"
	query := map[string]string{}
	if labelSelector != "" {
		query["listOptions.labelSelector"] = labelSelector
	}
	return scanPages(ctx, limit, continueToken, "ClusterWorkflowTemplate list", func(ctx context.Context, pageSize int, cursor string) ([]models.ClusterWorkflowTemplate, string, error) {
		var resp models.ClusterWorkflowTemplateList
		return fetchListPage(ctx, c, endpoint, query, pageSize, cursor, &resp, func() ([]models.ClusterWorkflowTemplate, string) {
			return resp.Items, resp.Metadata.Continue
		})
	}, func(item models.ClusterWorkflowTemplate) (ClusterTemplateSummary, bool) {
		name := item.Metadata.Name
		if name == "" {
			return ClusterTemplateSummary{}, false
		}
		return ClusterTemplateSummary{
			Name:       name,
			Entrypoint: item.Spec.Entrypoint,
		}, true
	})
}

func boundedLimit(limit, defaultLimit int) (int, error) {
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 0 || limit > maximumCollectionLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximumCollectionLimit)
	}
	return limit, nil
}

func setContinueQuery(query map[string]string, cursor string) {
	if cursor == "" {
		delete(query, "listOptions.continue")
		return
	}
	query["listOptions.continue"] = cursor
}

func fetchListPage[Item, Response any](ctx context.Context, client *Client, endpoint string, query map[string]string, pageSize int, cursor string, response *Response, unpack func() ([]Item, string)) ([]Item, string, error) {
	query["listOptions.limit"] = intString(pageSize)
	setContinueQuery(query, cursor)
	if err := client.doJSON(ctx, http.MethodGet, endpoint, query, nil, response); err != nil {
		return nil, "", err
	}
	items, next := unpack()
	return items, next, nil
}

func scanPages[Raw, Item any](ctx context.Context, limit int, initialCursor, resource string, fetch func(context.Context, int, string) ([]Raw, string, error), project func(Raw) (Item, bool)) (Page[Item], error) {
	result := Page[Item]{Items: make([]Item, 0, limit)}
	seen := map[string]struct{}{}
	if initialCursor != "" {
		seen[initialCursor] = struct{}{}
	}
	cursor := initialCursor
	for requestCount := 0; ; requestCount++ {
		if requestCount >= maximumBackendPages {
			return Page[Item]{}, fmt.Errorf("argo %s exceeded %d backend pages", resource, maximumBackendPages)
		}
		remaining := limit - len(result.Items)
		raw, next, err := fetch(ctx, remaining, cursor)
		if err != nil {
			return Page[Item]{}, err
		}
		if len(raw) > remaining {
			return Page[Item]{}, fmt.Errorf("argo %s returned %d items after a limit of %d was requested; cannot continue without dropping items", resource, len(raw), remaining)
		}
		for _, value := range raw {
			if item, ok := project(value); ok {
				result.Items = append(result.Items, item)
			}
		}
		if next != "" {
			if _, exists := seen[next]; exists {
				return Page[Item]{}, fmt.Errorf("argo repeated %s continuation token", resource)
			}
			seen[next] = struct{}{}
		}
		if len(result.Items) == limit || next == "" {
			result.Continue = next
			return result, nil
		}
		cursor = next
	}
}

func (c *Client) GetClusterWorkflowTemplate(ctx context.Context, name string) (*ClusterTemplateDetail, error) {
	endpoint := c.baseURL + "/api/v1/cluster-workflow-templates/" + url.PathEscape(name)
	var resp models.ClusterWorkflowTemplate
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp)
	if err != nil {
		return nil, err
	}
	return &ClusterTemplateDetail{
		ClusterTemplateSummary: ClusterTemplateSummary{
			Name:       resp.Metadata.Name,
			Entrypoint: resp.Spec.Entrypoint,
		},
		TemplateNames: templateNames(resp.Spec),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, query map[string]string, body, destination any) error {
	req, err := c.newRequest(ctx, method, endpoint, query, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
	}
	if destination == nil {
		var discarded json.RawMessage
		destination = &discarded
	}
	if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
		return fmt.Errorf("decode json from %s: %w", endpoint, err)
	}
	return nil
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

func workflowSummaryFromModel(item models.Workflow) WorkflowSummary {
	return WorkflowSummary{
		Name:       item.Metadata.Name,
		Namespace:  item.Metadata.Namespace,
		Status:     coalesce(item.Status.Phase, "Unknown"),
		Progress:   item.Status.Progress,
		StartedAt:  item.Status.StartedAt,
		FinishedAt: item.Status.FinishedAt,
	}
}

func templateSummaryFromModel(metadata models.ObjectMeta, spec models.WorkflowSpec, namespace string) TemplateSummary {
	return TemplateSummary{
		Name:       metadata.Name,
		Namespace:  coalesce(metadata.Namespace, namespace),
		Entrypoint: spec.Entrypoint,
	}
}

func templateNames(spec models.WorkflowSpec) []string {
	names := make([]string, 0, len(spec.Templates))
	for _, template := range spec.Templates {
		if template.Name != "" {
			names = append(names, template.Name)
		}
	}
	return names
}

func cronSchedules(spec models.CronWorkflowSpec) []string {
	schedules := make([]string, 0, len(spec.Schedules))
	for _, schedule := range spec.Schedules {
		if schedule != "" {
			schedules = append(schedules, schedule)
		}
	}
	if len(schedules) == 0 {
		if spec.Schedule != "" {
			return []string{spec.Schedule}
		}
	}
	return schedules
}

func cronSchedule(spec models.CronWorkflowSpec) string {
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

func renderParameters(parameters []models.Parameter) map[string]string {
	out := make(map[string]string, len(parameters))
	for _, parameter := range parameters {
		if parameter.Name == "" {
			continue
		}
		out[parameter.Name] = coalesce(
			renderParameterJSON(parameter.Value),
			renderParameterJSON(parameter.Default),
			renderParameterJSON(parameter.ValueFrom),
		)
	}
	return out
}

func renderParameterJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return fmt.Sprintf("%v", typed)
	case map[string]any, []any:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(data)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func nonnilStringMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	return input
}

type logEnvelope struct {
	Error   json.RawMessage `json:"error"`
	Result  json.RawMessage `json:"result"`
	PodName string          `json:"podName"`
	Content string          `json:"content"`
}

type logResult struct {
	PodName string `json:"podName"`
	Content string `json:"content"`
}

func emptyJSONValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return false
	}
	return len(object) == 0
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
