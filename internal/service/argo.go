package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	loom "github.com/CaliLuke/loom/pkg"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
	"github.com/CaliLuke/go-argo-mcp/internal/confirmation"
)

const (
	defaultLimit         = 50
	defaultLogMaxLines   = 200
	defaultCronHistLimit = 10
	maximumListLimit     = 200
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
	limit, err := collectionLimit(payload.Limit, defaultLimit)
	if err != nil {
		return nil, err
	}
	continueToken := pointerValue(payload.Continue)

	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := client.ListWorkflows(ctx, namespace, status, limit, continueToken)
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "list", resource: "Workflows", namespace: namespace})
	}

	workflows := make([]*genargo.WorkflowSummary, 0, len(page.Items))
	for _, item := range page.Items {
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
		HasMore:   page.Continue != "",
	}
	if page.Continue != "" {
		res.Continue = strPtr(page.Continue)
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
		return nil, mapArgoError(err, argoTarget{action: "get", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
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
		return nil, mapArgoError(err, argoTarget{action: "get logs for", resource: "Workflow", namespace: namespace, name: workflowName, listTool: "list_workflows"})
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
	switch {
	case total == 0:
		res.Note = strPtr(fmt.Sprintf("Argo returned no log entries for Workflow %q in namespace %q and container %q. The pods may have produced no logs, may have been removed, or logs may no longer be retained.", workflowName, namespace, container))
	case search != "" && matching == 0:
		res.Note = strPtr(fmt.Sprintf("No log entries matched %q among the %d entries returned by Argo.", search, total))
	case matching > returned:
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
		return deniedActionResult("terminate_workflow", "MCP_ALLOW_DESTRUCTIVE", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if dryRun {
		confirmationToken, err := s.confirmations.Issue("terminate_workflow:"+reason, namespace, name)
		if err != nil {
			return nil, genargo.MakeConfirmationInvalid(err)
		}
		res := actionResult("dry_run", fmt.Sprintf("Termination preview generated for Workflow %q in namespace %q; no Argo request was made.", name, namespace), namespace, name)
		res.Preview = strPtr(fmt.Sprintf("Would terminate Workflow %q in namespace %q for reason %q.", name, namespace, reason))
		res.Instructions = strPtr("Call terminate_workflow once with the same name, namespace, and reason, dry_run=false, and the returned confirmation_token.")
		res.ConfirmationToken = strPtr(confirmationToken)
		res.Reason = strPtr(reason)
		return res, nil
	}
	if s.policy.RequireConfirmation && !s.confirmations.Consume(token, "terminate_workflow:"+reason, namespace, name) {
		err := genargo.MakeConfirmationInvalid(fmt.Errorf("invalid confirmation token for terminate_workflow %s/%s", namespace, name))
		return nil, loom.WithErrorRemedy(err, &loom.ErrorRemedy{
			Code:        "argo.confirmation.refresh",
			SafeMessage: fmt.Sprintf("Confirmation for Workflow %q in namespace %q is invalid, expired, already used, or scoped to different inputs.", name, namespace),
			RetryHint:   "Run terminate_workflow again with the same name, namespace, and reason in dry-run mode, then use the new confirmation_token once. No Argo request was made.",
		})
	}
	if err := client.TerminateWorkflow(ctx, namespace, name); err != nil {
		return nil, mapArgoError(err, argoTarget{action: "terminate", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
	}
	res := actionResult("ok", fmt.Sprintf("Workflow %q in namespace %q was terminated.", name, namespace), namespace, name)
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
		return deniedActionResult("retry_workflow", "MCP_ALLOW_MUTATIONS", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.RetryWorkflow(ctx, namespace, name, restartSuccessful); err != nil {
		return nil, mapArgoError(err, argoTarget{action: "retry", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
	}
	res := actionResult("ok", fmt.Sprintf("Retry was initiated for Workflow %q in namespace %q.", name, namespace), namespace, name)
	res.RestartSuccessful = boolPtr(restartSuccessful)
	return res, nil
}

func (s *ArgoService) ListCronWorkflows(ctx context.Context, payload *genargo.ListCronWorkflowsPayload) (*genargo.ListCronWorkflowsResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	suspended := payload.Suspended
	limit, err := collectionLimit(payload.Limit, defaultLimit)
	if err != nil {
		return nil, err
	}
	continueToken := pointerValue(payload.Continue)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := client.ListCronWorkflows(ctx, namespace, suspended, limit, continueToken)
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "list", resource: "CronWorkflows", namespace: namespace})
	}
	out := make([]*genargo.CronWorkflowSummary, 0, len(page.Items))
	for _, item := range page.Items {
		summary := &genargo.CronWorkflowSummary{Name: item.Name, Namespace: item.Namespace}
		if item.Schedule != "" {
			summary.Schedule = strPtr(item.Schedule)
		}
		if len(item.Schedules) > 0 {
			summary.Schedules = item.Schedules
		}
		if item.Timezone != "" {
			summary.Timezone = strPtr(item.Timezone)
		}
		summary.Suspended = boolPtr(item.Suspended)
		out = append(out, summary)
	}
	res := &genargo.ListCronWorkflowsResult{
		CronWorkflows: out,
		Count:         len(out),
		Namespace:     strPtr(namespace),
		Source:        "argo",
		HasMore:       page.Continue != "",
	}
	if page.Continue != "" {
		res.Continue = strPtr(page.Continue)
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
		return nil, mapArgoError(err, argoTarget{action: "get", resource: "CronWorkflow", namespace: namespace, name: name, listTool: "list_cron_workflows"})
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
	if len(item.Schedules) > 0 {
		res.Schedules = item.Schedules
	}
	if item.Timezone != "" {
		res.Timezone = strPtr(item.Timezone)
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
		return deniedActionResult("toggle_cron_suspension", "MCP_ALLOW_MUTATIONS", namespace, name), nil
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.ToggleCronSuspension(ctx, namespace, name, payload.Suspend); err != nil {
		return nil, mapArgoError(err, argoTarget{action: suspensionAction(payload.Suspend), resource: "CronWorkflow", namespace: namespace, name: name, listTool: "list_cron_workflows"})
	}
	return actionResult("ok", fmt.Sprintf("CronWorkflow %q in namespace %q was %s.", name, namespace, suspensionVerb(payload.Suspend)), namespace, name), nil
}

func (s *ArgoService) ListWorkflowTemplates(ctx context.Context, payload *genargo.ListWorkflowTemplatesPayload) (*genargo.ListWorkflowTemplatesResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	labelSelector := stringPtrValue(payload.LabelSelector)
	limit, err := collectionLimit(payload.Limit, defaultLimit)
	if err != nil {
		return nil, err
	}
	continueToken := pointerValue(payload.Continue)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := client.ListWorkflowTemplates(ctx, namespace, labelSelector, limit, continueToken)
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "list", resource: "WorkflowTemplates", namespace: namespace})
	}
	out := make([]*genargo.TemplateSummary, 0, len(page.Items))
	for _, item := range page.Items {
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
		HasMore:   page.Continue != "",
	}
	if page.Continue != "" {
		res.Continue = strPtr(page.Continue)
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
		return nil, mapArgoError(err, argoTarget{action: "get", resource: "WorkflowTemplate", namespace: namespace, name: name, listTool: "list_workflow_templates"})
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
	limit, err := collectionLimit(payload.Limit, defaultLimit)
	if err != nil {
		return nil, err
	}
	continueToken := pointerValue(payload.Continue)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := client.ListClusterWorkflowTemplates(ctx, labelSelector, limit, continueToken)
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "list", resource: "ClusterWorkflowTemplates"})
	}
	out := make([]*genargo.ClusterWorkflowTemplateSummary, 0, len(page.Items))
	for _, item := range page.Items {
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
		HasMore:   page.Continue != "",
	}
	if page.Continue != "" {
		res.Continue = strPtr(page.Continue)
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
		return nil, mapArgoError(err, argoTarget{action: "get", resource: "ClusterWorkflowTemplate", name: name, listTool: "list_cluster_workflow_templates"})
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
	limit, err := collectionLimit(payload.Limit, defaultCronHistLimit)
	if err != nil {
		return nil, err
	}
	continueToken := pointerValue(payload.Continue)
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := client.GetCronHistory(ctx, namespace, name, limit, continueToken)
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "get execution history for", resource: "CronWorkflow", namespace: namespace, name: name, listTool: "list_cron_workflows"})
	}
	out := make([]*genargo.CronHistoryEntry, 0, len(page.Items))
	for _, entry := range page.Items {
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
	res := &genargo.CronHistoryResult{
		Name:      name,
		Namespace: strPtr(namespace),
		History:   out,
		Count:     len(out),
		Source:    "argo",
		HasMore:   page.Continue != "",
	}
	if page.Continue != "" {
		res.Continue = strPtr(page.Continue)
	}
	return res, nil
}

func collectionLimit(limit *int, defaultValue int) (int, error) {
	if limit == nil || *limit == 0 {
		return defaultValue, nil
	}
	if *limit < 0 || *limit > maximumListLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximumListLimit)
	}
	return *limit, nil
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

type argoTarget struct {
	action    string
	resource  string
	namespace string
	name      string
	listTool  string
}

func (t argoTarget) describe() string {
	target := t.resource
	if t.name != "" {
		target = fmt.Sprintf("%s %q", target, t.name)
	}
	if t.namespace != "" {
		target += fmt.Sprintf(" in namespace %q", t.namespace)
	}
	return target
}

func (t argoTarget) notFoundHint() string {
	if t.listTool != "" {
		if t.namespace != "" {
			return fmt.Sprintf("Call %s for namespace %q to discover valid names, or correct the name and namespace.", t.listTool, t.namespace)
		}
		return fmt.Sprintf("Call %s to discover valid names, or correct the name.", t.listTool)
	}
	return "Check the requested resource and server configuration before retrying."
}

func mapArgoError(err error, target argoTarget) error {
	var httpErr *argoapi.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusNotFound:
			return loom.WithErrorRemedy(genargo.MakeArgoNotFound(err), &loom.ErrorRemedy{
				Code:        "argo.resource.not_found",
				SafeMessage: fmt.Sprintf("%s was not found.", target.describe()),
				RetryHint:   target.notFoundHint(),
			})
		case http.StatusUnauthorized, http.StatusForbidden:
			return loom.WithErrorRemedy(genargo.MakeArgoAccessDenied(err), &loom.ErrorRemedy{
				Code:        "argo.access.denied",
				SafeMessage: fmt.Sprintf("Argo denied access while trying to %s %s.", target.action, target.describe()),
				RetryHint:   "Check the Argo credentials and RBAC permissions used by this server before retrying.",
			})
		default:
			if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
				return loom.WithErrorRemedy(genargo.MakeArgoRequestRejected(err), &loom.ErrorRemedy{
					Code:        "argo.request.rejected",
					SafeMessage: fmt.Sprintf("Argo rejected the request to %s %s.", target.action, target.describe()),
					RetryHint:   "Check the tool inputs and the resource's current state before retrying.",
				})
			}
		}
	}
	return loom.WithErrorRemedy(genargo.MakeArgoAPIError(err), &loom.ErrorRemedy{
		Code:        "argo.api.retry",
		SafeMessage: fmt.Sprintf("Argo failed while trying to %s %s.", target.action, target.describe()),
		RetryHint:   "Verify Argo connectivity and credentials, then retry the same request.",
	})
}

func (s *ArgoService) authorizeNamespace(namespace string) error {
	if namespaceInList(namespace, s.policy.DeniedNamespaces) {
		return namespaceDeniedError(namespace, fmt.Errorf("namespace %q is explicitly denied", namespace))
	}
	if len(s.policy.AllowedNamespaces) > 0 && !namespaceInList(namespace, s.policy.AllowedNamespaces) {
		return namespaceDeniedError(namespace, fmt.Errorf("namespace %q is not in the allow list", namespace))
	}
	return nil
}

func namespaceDeniedError(namespace string, cause error) error {
	return loom.WithErrorRemedy(genargo.MakeNamespaceDenied(cause), &loom.ErrorRemedy{
		Code:        "argo.namespace.denied",
		SafeMessage: fmt.Sprintf("Namespace %q is denied by the MCP namespace policy.", namespace),
		RetryHint:   "Use a namespace allowed by MCP_NAMESPACES_ALLOW and absent from MCP_NAMESPACES_DENY. No Argo request was made.",
	})
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

func deniedActionResult(toolName, setting, namespace, name string) *genargo.ActionResult {
	res := actionResult("denied", fmt.Sprintf("%s is disabled by server policy.", toolName), namespace, name)
	res.Instructions = strPtr(fmt.Sprintf("Ask the server operator to set %s=true and restart the server. No Argo request was made.", setting))
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

func suspensionAction(suspend bool) string {
	if suspend {
		return "suspend"
	}
	return "resume"
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
