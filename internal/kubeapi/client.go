package kubeapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
)

const maxResponseBytes = 4 << 20
const maxResultBytes = 1 << 20

type Config struct {
	BaseURL    string
	Token      string
	CAPEM      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Client struct {
	baseURL, token  string
	http            *http.Client
	diagnoseTimeout time.Duration
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(cfg.BaseURL, "#") {
		return nil, fmt.Errorf("invalid Kubernetes API URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme != "https" && (u.Scheme != "http" || ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("kubernetes API URL must use HTTPS (HTTP is allowed only for loopback)")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	} else {
		clone := *client
		client = &clone
	}
	if cfg.CAPEM != "" {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(cfg.CAPEM)) {
			return nil, fmt.Errorf("invalid Kubernetes CA PEM")
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok && client.Transport != nil {
			return nil, errors.New("kubernetes CA PEM requires an HTTP transport")
		}
		if transport == nil {
			defaultTransport, isHTTPTransport := http.DefaultTransport.(*http.Transport)
			if !isHTTPTransport {
				return nil, errors.New("kubernetes CA PEM requires the default HTTP transport")
			}
			transport = defaultTransport
		}
		transport = transport.Clone()
		tlsConfig := transport.TLSClientConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tlsConfig = tlsConfig.Clone()
		}
		tlsConfig.RootCAs = roots
		transport.TLSClientConfig = tlsConfig
		client.Transport = transport
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	client.Timeout = cfg.Timeout
	originScheme, originHost := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || strings.ToLower(req.URL.Scheme) != originScheme || strings.ToLower(req.URL.Host) != originHost {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &Client{baseURL: strings.TrimRight(cfg.BaseURL, "/"), token: cfg.Token, http: client, diagnoseTimeout: 10 * time.Second}, nil
}

func (c *Client) Diagnose(ctx context.Context, namespace, name, workflowUID, podName string, limit int, continueToken string) (*genargo.WorkflowPodDiagnosticsResult, error) {
	parent := ctx
	diagnoseTimeout := c.diagnoseTimeout
	if diagnoseTimeout <= 0 {
		diagnoseTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, diagnoseTimeout)
	defer cancel()
	if err := validateSegment(namespace); err != nil {
		return nil, responseError(err)
	}
	if err := validateSegment(name); err != nil {
		return nil, responseError(err)
	}
	if workflowUID == "" {
		return nil, responseError(errors.New("workflow UID is missing"))
	}
	if limit < 1 || limit > 50 {
		return nil, responseError(errors.New("limit must be between 1 and 50"))
	}
	if podName != "" && continueToken != "" {
		return nil, responseError(errors.New("continue is invalid with pod_name"))
	}

	var pods []pod
	next := ""
	if podName != "" {
		if err := validateSegment(podName); err != nil {
			return nil, responseError(err)
		}
		var item pod
		if err := c.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(podName), nil, &item); err != nil {
			return nil, mainRequestError(parent, err)
		}
		pods = []pod{item}
	} else {
		query := url.Values{"labelSelector": {"workflows.argoproj.io/workflow=" + name}, "limit": {strconv.Itoa(limit)}}
		if continueToken != "" {
			query.Set("continue", continueToken)
		}
		var page podList
		if err := c.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods", query, &page); err != nil {
			return nil, mainRequestError(parent, err)
		}
		if len(page.Items) > limit {
			return nil, responseError(errors.New("kubernetes returned more pods than requested"))
		}
		pods, next = page.Items, page.Metadata.Continue
	}

	result := &genargo.WorkflowPodDiagnosticsResult{Namespace: namespace, Name: name, Pods: make([]*genargo.WorkflowPodDiagnostic, 0, len(pods)), Source: "kubernetes"}
	for _, item := range pods {
		if item.Metadata.Namespace != namespace || item.Metadata.Labels["workflows.argoproj.io/workflow"] != name || !ownedByWorkflow(item.Metadata.OwnerReferences, workflowUID) {
			if podName != "" {
				return nil, responseError(errors.New("pod does not belong to the requested workflow"))
			}
			continue
		}
		if validateSegment(item.Metadata.Name) != nil || validateSegment(item.Metadata.UID) != nil || podName != "" && item.Metadata.Name != podName {
			return nil, responseError(errors.New("kubernetes pod identity is invalid"))
		}
		projected := projectPod(item)
		events, eventsTruncated, fieldsTruncated, err := c.events(ctx, namespace, item.Metadata.UID)
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		if ctx.Err() != nil {
			return nil, genargo.MakeKubernetesAPIError(errors.New("kubernetes diagnostics deadline exceeded"))
		}
		if err != nil {
			message := eventErrorMessage(err)
			projected.EventsError = &message
		} else {
			projected.Events, projected.EventsTruncated = events, eventsTruncated
			projected.Truncated = projected.Truncated || fieldsTruncated
		}
		result.Pods = append(result.Pods, projected)
		result.Truncated = result.Truncated || projected.Truncated
	}
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	if ctx.Err() != nil {
		return nil, genargo.MakeKubernetesAPIError(errors.New("kubernetes diagnostics deadline exceeded"))
	}
	result.Count = len(result.Pods)
	result.HasMore = next != ""
	if result.HasMore {
		result.Continue = &next
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxResultBytes {
		return nil, responseError(errors.New("diagnostics result exceeds 1 MiB; lower limit or select pod_name"))
	}
	return result, nil
}

func (c *Client) events(ctx context.Context, namespace, uid string) ([]*genargo.PodDiagnosticEvent, bool, bool, error) {
	query := url.Values{"fieldSelector": {"involvedObject.uid=" + uid}, "limit": {"20"}}
	var page eventList
	if err := c.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/events", query, &page); err != nil {
		return nil, false, false, err
	}
	if len(page.Items) > 20 {
		return nil, false, false, responseError(errors.New("kubernetes returned more events than requested"))
	}
	out := make([]*genargo.PodDiagnosticEvent, 0, len(page.Items))
	fieldsTruncated := false
	for _, event := range page.Items {
		if event.Metadata.Namespace != namespace || event.InvolvedObject.Namespace != namespace || event.InvolvedObject.UID != uid {
			continue
		}
		message, clipped := truncate(event.Message, 4096)
		fieldsTruncated = fieldsTruncated || clipped
		out = append(out, &genargo.PodDiagnosticEvent{Type: clippedString(event.Type, 256, &fieldsTruncated), Reason: clippedString(event.Reason, 256, &fieldsTruncated), Message: message, Count: event.Count, FirstTimestamp: clippedString(event.FirstTimestamp, 256, &fieldsTruncated), LastTimestamp: clippedString(event.LastTimestamp, 256, &fieldsTruncated)})
	}
	return out, page.Metadata.Continue != "", fieldsTruncated, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return responseError(err)
	}
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return genargo.MakeKubernetesAPIError(errors.New("kubernetes request failed"))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, maxResponseBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return genargo.MakeKubernetesAPIError(errors.New("kubernetes response read failed"))
	}
	if len(body) > maxResponseBytes {
		return responseError(errors.New("kubernetes response exceeds 4 MiB"))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return responseError(errors.New("kubernetes returned malformed JSON"))
	}
	return nil
}

func statusError(status int) error {
	switch {
	case status == 401 || status == 403:
		return genargo.MakeKubernetesAccessDenied(errors.New("kubernetes access denied"))
	case status == 404:
		return genargo.MakeKubernetesNotFound(errors.New("kubernetes resource not found"))
	case status == 429 || status >= 500:
		return genargo.MakeKubernetesAPIError(errors.New("kubernetes API temporarily unavailable"))
	default:
		return responseError(errors.New("kubernetes rejected the request"))
	}
}
func responseError(err error) error { return genargo.MakeKubernetesResponseError(err) }
func mainRequestError(parent context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return genargo.MakeKubernetesAPIError(errors.New("kubernetes request timed out"))
	}
	return err
}
func eventErrorMessage(err error) string {
	if strings.Contains(err.Error(), "kubernetes_access_denied") || strings.Contains(strings.ToLower(err.Error()), "denied") {
		return "Kubernetes denied access to pod events; check events RBAC."
	}
	return "Kubernetes pod events are unavailable; check connectivity and retry."
}

type listMeta struct {
	Continue string `json:"continue"`
}
type objectMeta struct {
	Name, Namespace, UID string
	Labels               map[string]string `json:"labels"`
	OwnerReferences      []ownerReference  `json:"ownerReferences"`
}
type ownerReference struct {
	APIVersion string `json:"apiVersion"`
	Kind, UID  string
}
type podList struct {
	Metadata listMeta `json:"metadata"`
	Items    []pod    `json:"items"`
}
type pod struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		NodeName string `json:"nodeName"`
	} `json:"spec"`
	Status podStatus `json:"status"`
}
type podStatus struct {
	Phase                 string
	Conditions            []condition
	InitContainerStatuses []containerStatus `json:"initContainerStatuses"`
	ContainerStatuses     []containerStatus `json:"containerStatuses"`
}
type condition struct{ Type, Status, Reason, Message string }
type containerStatus struct {
	Name             string
	RestartCount     int `json:"restartCount"`
	Ready            bool
	State, LastState containerStates
}
type containerStates struct {
	Waiting    *stateWaiting
	Running    *stateRunning
	Terminated *stateTerminated
}
type stateWaiting struct{ Reason, Message string }
type stateRunning struct {
	StartedAt string `json:"startedAt"`
}
type stateTerminated struct {
	ExitCode                               int `json:"exitCode"`
	Reason, Message, StartedAt, FinishedAt string
}
type eventList struct {
	Metadata listMeta `json:"metadata"`
	Items    []event  `json:"items"`
}
type event struct {
	Metadata       objectMeta `json:"metadata"`
	InvolvedObject struct {
		UID       string `json:"uid"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
	Type, Reason, Message string
	Count                 int
	FirstTimestamp        string `json:"firstTimestamp"`
	LastTimestamp         string `json:"lastTimestamp"`
}

func ownedByWorkflow(refs []ownerReference, uid string) bool {
	for _, ref := range refs {
		if ref.Kind == "Workflow" && ref.UID == uid && (ref.APIVersion == "" || strings.HasPrefix(ref.APIVersion, "argoproj.io/")) {
			return true
		}
	}
	return false
}
func projectPod(item pod) *genargo.WorkflowPodDiagnostic {
	truncated := false
	out := &genargo.WorkflowPodDiagnostic{Name: clippedString(item.Metadata.Name, 256, &truncated), UID: clippedString(item.Metadata.UID, 256, &truncated), Phase: clippedString(item.Status.Phase, 256, &truncated), NodeName: clippedString(item.Spec.NodeName, 256, &truncated), Conditions: []*genargo.PodCondition{}, InitContainers: []*genargo.ContainerDiagnostic{}, Containers: []*genargo.ContainerDiagnostic{}, Events: []*genargo.PodDiagnosticEvent{}}
	conditions := item.Status.Conditions
	if len(conditions) > 50 {
		conditions = conditions[:50]
		truncated = true
	}
	for _, value := range conditions {
		message, cut := truncate(value.Message, 4096)
		truncated = truncated || cut
		out.Conditions = append(out.Conditions, &genargo.PodCondition{Type: clippedString(value.Type, 256, &truncated), Status: clippedString(value.Status, 256, &truncated), Reason: clippedString(value.Reason, 256, &truncated), Message: message})
	}
	out.InitContainers = projectContainers(item.Status.InitContainerStatuses, &truncated)
	out.Containers = projectContainers(item.Status.ContainerStatuses, &truncated)
	out.Truncated = truncated
	return out
}
func projectContainers(values []containerStatus, truncated *bool) []*genargo.ContainerDiagnostic {
	if len(values) > 50 {
		values = values[:50]
		*truncated = true
	}
	out := make([]*genargo.ContainerDiagnostic, 0, len(values))
	for _, v := range values {
		out = append(out, &genargo.ContainerDiagnostic{Name: clippedString(v.Name, 256, truncated), RestartCount: v.RestartCount, Ready: v.Ready, State: projectState(v.State, truncated), LastState: projectState(v.LastState, truncated)})
	}
	return out
}
func projectState(value containerStates, truncated *bool) *genargo.ContainerState {
	out := &genargo.ContainerState{Status: "unknown", Reason: "", Message: ""}
	if value.Waiting != nil {
		out.Status = "waiting"
		out.Reason = clippedString(value.Waiting.Reason, 256, truncated)
		out.Message = clippedString(value.Waiting.Message, 4096, truncated)
	} else if value.Running != nil {
		out.Status = "running"
		if value.Running.StartedAt != "" {
			out.StartedAt = &value.Running.StartedAt
		}
	} else if value.Terminated != nil {
		out.Status = "terminated"
		out.Reason = clippedString(value.Terminated.Reason, 256, truncated)
		out.Message = clippedString(value.Terminated.Message, 4096, truncated)
		out.ExitCode = &value.Terminated.ExitCode
		if value.Terminated.StartedAt != "" {
			out.StartedAt = &value.Terminated.StartedAt
		}
		if value.Terminated.FinishedAt != "" {
			out.FinishedAt = &value.Terminated.FinishedAt
		}
	}
	return out
}
func clippedString(value string, max int, clipped *bool) string {
	out, cut := truncate(value, max)
	*clipped = *clipped || cut
	return out
}
func truncate(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	value = value[:max]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
func validateSegment(value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.Contains(strings.ToLower(value), "%2f") || strings.Contains(strings.ToLower(value), "%5c") {
		return errors.New("invalid Kubernetes path segment")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return errors.New("invalid Kubernetes path segment")
		}
	}
	return nil
}
