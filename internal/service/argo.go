package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/confirmation"
)

const (
	defaultLimit         = 50
	defaultLogMaxLines   = 200
	defaultCronHistLimit = 10
)

type Policy struct {
	AllowMutations      bool
	AllowDestructive    bool
	RequireConfirmation bool
	AllowedNamespaces   []string
	DeniedNamespaces    []string
}

type ArgoService struct {
	client           *argoapi.Client
	defaultNamespace string
	policy           Policy
	confirmations    *confirmation.Manager
}

type ArgoServiceConfig struct {
	Client           *argoapi.Client
	DefaultNamespace string
	Policy           Policy
	Confirmations    *confirmation.Manager
}

func NewArgoService(config ArgoServiceConfig) *ArgoService {
	namespace := config.DefaultNamespace
	if namespace == "" {
		namespace = "default"
	}
	confirmations := config.Confirmations
	if confirmations == nil {
		confirmations = confirmation.New(confirmation.Config{})
	}
	return &ArgoService{
		client:           config.Client,
		defaultNamespace: namespace,
		policy:           config.Policy,
		confirmations:    confirmations,
	}
}

func (s *ArgoService) ListWorkflows(ctx context.Context, payload *genargo.ListWorkflowsPayload) (*genargo.ListWorkflowsResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	status := stringPtrValue(payload.Status)
	limit := intDefault(payload.Limit, defaultLimit)

	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	items, err := client.ListWorkflows(ctx, namespace, status, limit)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}

	workflows := make([]*genargo.WorkflowSummary, 0, len(items))
	for _, item := range items {
		summary := &genargo.WorkflowSummary{
			Name:      item.Name,
			Namespace: item.Namespace,
			Status:    item.Status,
		}
		if item.Progress != "" {
			summary.Progress = strPtr(item.Progress)
		}
		if item.StartedAt != "" {
			summary.StartedAt = strPtr(item.StartedAt)
		}
		if item.FinishedAt != "" {
			summary.FinishedAt = strPtr(item.FinishedAt)
		}
		if duration := formatDuration(item.StartedAt, item.FinishedAt); duration != "" {
			summary.Duration = strPtr(duration)
		}
		workflows = append(workflows, summary)
	}
	res := &genargo.ListWorkflowsResult{
		Workflows: workflows,
		Count:     len(workflows),
		Namespace: strPtr(namespace),
		Source:    "argo",
	}
	if status != "" {
		res.Status = strPtr(status)
	}
	return res, nil
}

func (s *ArgoService) GetWorkflow(ctx context.Context, payload *genargo.GetWorkflowPayload) (*genargo.WorkflowDetailResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	detail, err := client.GetWorkflow(ctx, namespace, name)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}

	res := &genargo.WorkflowDetailResult{
		Name:        detail.Summary.Name,
		Namespace:   detail.Summary.Namespace,
		Status:      coalesce(detail.Summary.Status, "Unknown"),
		Labels:      detail.Labels,
		Annotations: detail.Annotations,
		Parameters:  detail.Parameters,
		Outputs:     detail.Outputs,
	}
	if detail.Summary.Progress != "" {
		res.Progress = strPtr(detail.Summary.Progress)
	}
	if detail.Summary.StartedAt != "" {
		res.StartedAt = strPtr(detail.Summary.StartedAt)
	}
	if detail.Summary.FinishedAt != "" {
		res.FinishedAt = strPtr(detail.Summary.FinishedAt)
	}
	if detail.Message != "" {
		res.Message = strPtr(detail.Message)
	}
	if duration := formatDuration(detail.Summary.StartedAt, detail.Summary.FinishedAt); duration != "" {
		res.Duration = strPtr(duration)
	}
	return res, nil
}

func (s *ArgoService) GetWorkflowLogs(ctx context.Context, payload *genargo.GetWorkflowLogsPayload) (*genargo.WorkflowLogsResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	workflowName := strings.TrimSpace(payload.WorkflowName)
	if workflowName == "" {
		return nil, fmt.Errorf("workflow_name is required")
	}
	container := stringDefault(payload.Container, "main")
	search := stringPtrValue(payload.Search)
	maxLines := payload.MaxLines
	podName := stringPtrValue(payload.PodName)

	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	entries, err := client.GetWorkflowLogs(ctx, namespace, workflowName, podName, container)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}

	total := len(entries)
	filtered := entries
	if search != "" {
		filtered = filterLogs(entries, search)
	}
	matching := len(filtered)
	if maxLines > 0 && len(filtered) > maxLines {
		filtered = filtered[len(filtered)-maxLines:]
	}
	returned := len(filtered)
	rendered := renderLogs(filtered)

	res := &genargo.WorkflowLogsResult{
		Namespace:     namespace,
		Workflow:      workflowName,
		Container:     container,
		TotalLines:    total,
		MatchingLines: matching,
		ReturnedLines: returned,
		Logs:          rendered,
	}
	if podName != "" {
		res.Pod = strPtr(podName)
	}
	if search != "" {
		res.SearchTerm = strPtr(search)
	}
	if maxLines > 0 {
		res.MaxLines = intPtr(maxLines)
	}
	if matching > returned {
		res.Note = strPtr(fmt.Sprintf("Showing last %d of %d matching lines", returned, matching))
	}
	return res, nil
}

func (s *ArgoService) TerminateWorkflow(ctx context.Context, payload *genargo.TerminateWorkflowPayload) (*genargo.ActionResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	reason := strings.TrimSpace(payload.Reason)
	if name == "" || reason == "" {
		return nil, fmt.Errorf("name and reason are required")
	}
	dryRun := boolPtrDefault(payload.DryRun, true)
	token := stringPtrValue(payload.ConfirmationToken)

	if !s.policy.AllowDestructive {
		return actionResult("denied", "Destructive operations are not allowed by configuration", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if dryRun {
		confirmationToken, err := s.confirmations.Issue("terminate_workflow", namespace, name)
		if err != nil {
			return nil, genargo.MakeConfirmationInvalid(err)
		}
		res := actionResult("dry_run", "Preview generated for workflow termination", namespace, name)
		res.Preview = strPtr(fmt.Sprintf("Would terminate workflow %s in namespace %s for reason: %s", name, namespace, reason))
		res.Instructions = strPtr("Call again once with dry_run=false and the returned confirmation_token")
		res.ConfirmationToken = strPtr(confirmationToken)
		res.Reason = strPtr(reason)
		return res, nil
	}
	if s.policy.RequireConfirmation && !s.confirmations.Consume(token, "terminate_workflow", namespace, name) {
		return nil, genargo.MakeConfirmationInvalid(fmt.Errorf("invalid confirmation token for terminate_workflow %s/%s", namespace, name))
	}
	if err := client.TerminateWorkflow(ctx, namespace, name); err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	res := actionResult("ok", "Workflow terminated", namespace, name)
	res.Reason = strPtr(reason)
	return res, nil
}

func (s *ArgoService) RetryWorkflow(ctx context.Context, payload *genargo.RetryWorkflowPayload) (*genargo.ActionResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	restartSuccessful := boolPtrDefault(payload.RestartSuccessful, false)
	if !s.policy.AllowMutations {
		return actionResult("denied", "Mutation operations are not allowed by configuration", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.RetryWorkflow(ctx, namespace, name, restartSuccessful); err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	res := actionResult("ok", "Workflow retry initiated", namespace, name)
	res.RestartSuccessful = boolPtr(restartSuccessful)
	return res, nil
}

func (s *ArgoService) ListCronWorkflows(ctx context.Context, payload *genargo.ListCronWorkflowsPayload) (*genargo.ListCronWorkflowsResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	suspended := payload.Suspended
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	items, err := client.ListCronWorkflows(ctx, namespace, suspended)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	out := make([]*genargo.CronWorkflowSummary, 0, len(items))
	for _, item := range items {
		summary := &genargo.CronWorkflowSummary{Name: item.Name, Namespace: item.Namespace}
		if item.Schedule != "" {
			summary.Schedule = strPtr(item.Schedule)
		}
		summary.Suspended = boolPtr(item.Suspended)
		out = append(out, summary)
	}
	res := &genargo.ListCronWorkflowsResult{
		CronWorkflows: out,
		Count:         len(out),
		Namespace:     strPtr(namespace),
		Source:        "argo",
	}
	if suspended != nil {
		res.Suspended = suspended
	}
	return res, nil
}

func (s *ArgoService) GetCronWorkflow(ctx context.Context, payload *genargo.GetCronWorkflowPayload) (*genargo.CronWorkflowDetailResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	item, err := client.GetCronWorkflow(ctx, namespace, name)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	res := &genargo.CronWorkflowDetailResult{
		Name:      item.Name,
		Source:    "argo",
		Namespace: strPtr(item.Namespace),
		Suspended: boolPtr(item.Suspended),
	}
	if item.Schedule != "" {
		res.Schedule = strPtr(item.Schedule)
	}
	if item.LastScheduledTime != "" {
		res.LastScheduledTime = strPtr(item.LastScheduledTime)
	}
	if item.NextScheduledTime != "" {
		res.NextScheduledTime = strPtr(item.NextScheduledTime)
	}
	return res, nil
}

func (s *ArgoService) ToggleCronSuspension(ctx context.Context, payload *genargo.ToggleCronSuspensionPayload) (*genargo.ActionResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if !s.policy.AllowMutations {
		return actionResult("denied", "Mutation operations are not allowed by configuration", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.ToggleCronSuspension(ctx, namespace, name, payload.Suspend); err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	return actionResult("ok", fmt.Sprintf("CronWorkflow %s", suspensionVerb(payload.Suspend)), namespace, name), nil
}

func (s *ArgoService) ListWorkflowTemplates(ctx context.Context, payload *genargo.ListWorkflowTemplatesPayload) (*genargo.ListWorkflowTemplatesResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	labelSelector := stringPtrValue(payload.LabelSelector)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	items, err := client.ListWorkflowTemplates(ctx, namespace, labelSelector)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	out := make([]*genargo.TemplateSummary, 0, len(items))
	for _, item := range items {
		summary := &genargo.TemplateSummary{Name: item.Name}
		if item.Namespace != "" {
			summary.Namespace = strPtr(item.Namespace)
		}
		if item.Entrypoint != "" {
			summary.Entrypoint = strPtr(item.Entrypoint)
		}
		out = append(out, summary)
	}
	res := &genargo.ListWorkflowTemplatesResult{
		Templates: out,
		Count:     len(out),
		Namespace: strPtr(namespace),
		Source:    "argo",
	}
	if labelSelector != "" {
		res.LabelSelector = strPtr(labelSelector)
	}
	return res, nil
}

func (s *ArgoService) GetWorkflowTemplate(ctx context.Context, payload *genargo.GetWorkflowTemplatePayload) (*genargo.WorkflowTemplateDetailResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	item, err := client.GetWorkflowTemplate(ctx, namespace, name)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	res := &genargo.WorkflowTemplateDetailResult{
		Name:          item.Name,
		Source:        "argo",
		TemplateNames: item.TemplateNames,
	}
	if item.Namespace != "" {
		res.Namespace = strPtr(item.Namespace)
	}
	if item.Entrypoint != "" {
		res.Entrypoint = strPtr(item.Entrypoint)
	}
	return res, nil
}

func (s *ArgoService) ListClusterWorkflowTemplates(ctx context.Context, payload *genargo.ListClusterWorkflowTemplatesPayload) (*genargo.ListClusterWorkflowTemplatesResult, error) {
	labelSelector := stringPtrValue(payload.LabelSelector)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	items, err := client.ListClusterWorkflowTemplates(ctx, labelSelector)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	out := make([]*genargo.ClusterWorkflowTemplateSummary, 0, len(items))
	for _, item := range items {
		summary := &genargo.ClusterWorkflowTemplateSummary{Name: item.Name}
		if item.Entrypoint != "" {
			summary.Entrypoint = strPtr(item.Entrypoint)
		}
		out = append(out, summary)
	}
	res := &genargo.ListClusterWorkflowTemplatesResult{
		Templates: out,
		Count:     len(out),
		Source:    "argo",
	}
	if labelSelector != "" {
		res.LabelSelector = strPtr(labelSelector)
	}
	return res, nil
}

func (s *ArgoService) GetClusterWorkflowTemplate(ctx context.Context, payload *genargo.GetClusterWorkflowTemplatePayload) (*genargo.ClusterWorkflowTemplateDetailResult, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	item, err := client.GetClusterWorkflowTemplate(ctx, name)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	res := &genargo.ClusterWorkflowTemplateDetailResult{
		Name:          item.Name,
		Source:        "argo",
		TemplateNames: item.TemplateNames,
	}
	if item.Entrypoint != "" {
		res.Entrypoint = strPtr(item.Entrypoint)
	}
	return res, nil
}

func (s *ArgoService) GetCronHistory(ctx context.Context, payload *genargo.GetCronHistoryPayload) (*genargo.CronHistoryResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	limit := intDefault(payload.Limit, defaultCronHistLimit)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	entries, err := client.GetCronHistory(ctx, namespace, name, limit)
	if err != nil {
		return nil, genargo.MakeArgoAPIError(err)
	}
	out := make([]*genargo.CronHistoryEntry, 0, len(entries))
	for _, entry := range entries {
		item := &genargo.CronHistoryEntry{Name: entry.Name}
		if entry.Status != "" {
			item.Status = strPtr(entry.Status)
		}
		if entry.StartedAt != "" {
			item.StartedAt = strPtr(entry.StartedAt)
		}
		if entry.FinishedAt != "" {
			item.FinishedAt = strPtr(entry.FinishedAt)
		}
		if duration := formatDuration(entry.StartedAt, entry.FinishedAt); duration != "" {
			item.Duration = strPtr(duration)
		}
		out = append(out, item)
	}
	return &genargo.CronHistoryResult{
		Name:      name,
		Namespace: strPtr(namespace),
		History:   out,
		Count:     len(out),
		Source:    "argo",
	}, nil
}

func (s *ArgoService) namespace(v *string) string {
	if value := stringPtrValue(v); value != "" {
		return value
	}
	return s.defaultNamespace
}

func (s *ArgoService) enabled() bool { return s.client != nil && s.client.Enabled() }

func (s *ArgoService) requireClient() (*argoapi.Client, error) {
	if !s.enabled() {
		return nil, genargo.MakeConfigurationError(fmt.Errorf("ARGO_BASE_URL is required"))
	}
	return s.client, nil
}

func (s *ArgoService) authorizeNamespace(namespace string) error {
	if namespaceInList(namespace, s.policy.DeniedNamespaces) {
		return genargo.MakeNamespaceDenied(fmt.Errorf("namespace %q is explicitly denied", namespace))
	}
	if len(s.policy.AllowedNamespaces) > 0 && !namespaceInList(namespace, s.policy.AllowedNamespaces) {
		return genargo.MakeNamespaceDenied(fmt.Errorf("namespace %q is not in the allow list", namespace))
	}
	return nil
}

func namespaceInList(namespace string, entries []string) bool {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "*" || entry == namespace {
			return true
		}
	}
	return false
}

func actionResult(status, message, namespace, name string) *genargo.ActionResult {
	res := &genargo.ActionResult{
		Status:  status,
		Message: message,
	}
	if namespace != "" {
		res.Namespace = strPtr(namespace)
	}
	if name != "" {
		res.Name = strPtr(name)
	}
	return res
}

func filterLogs(entries []argoapi.WorkflowLogEntry, search string) []argoapi.WorkflowLogEntry {
	lowered := strings.ToLower(search)
	out := make([]argoapi.WorkflowLogEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Content), lowered) || strings.Contains(strings.ToLower(entry.PodName), lowered) {
			out = append(out, entry)
		}
	}
	return out
}

func renderLogs(entries []argoapi.WorkflowLogEntry) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.PodName != "" {
			lines = append(lines, fmt.Sprintf("[%s] %s", entry.PodName, entry.Content))
		} else {
			lines = append(lines, entry.Content)
		}
	}
	return strings.Join(lines, "\n")
}

func formatDuration(startedAt, finishedAt string) string {
	if startedAt == "" {
		return ""
	}
	start, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}
	end := time.Now().UTC()
	if finishedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, finishedAt); err == nil {
			end = parsed
		}
	}
	if end.Before(start) {
		return ""
	}
	return end.Sub(start).Truncate(time.Second).String()
}

func suspensionVerb(suspend bool) string {
	if suspend {
		return "suspended"
	}
	return "resumed"
}

func strPtr(value string) *string { return &value }
func intPtr(value int) *int       { return &value }
func boolPtr(value bool) *bool    { return &value }

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func intDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func stringDefault(value, fallback string) string {
	if got := strings.TrimSpace(value); got != "" {
		return got
	}
	return fallback
}

func boolPtrDefault(value *bool, fallback bool) bool {
	if value != nil {
		return *value
	}
	return fallback
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
