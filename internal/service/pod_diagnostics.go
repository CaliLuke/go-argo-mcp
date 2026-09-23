package service

import (
	"context"
	"errors"
	"strings"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
)

func (s *ArgoService) GetWorkflowPodDiagnostics(ctx context.Context, payload *genargo.GetWorkflowPodDiagnosticsPayload) (*genargo.WorkflowPodDiagnosticsResult, error) {
	namespace := s.namespace(payload.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, invalidInput("name is required")
	}
	podName, token := pointerValue(payload.PodName), pointerValue(payload.Continue)
	if !safeReadSegment(namespace) || !safeReadSegment(name) || podName != "" && !safeReadSegment(podName) {
		return nil, invalidInput("namespace, name, and pod_name must be safe path segments")
	}
	limit := payload.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return nil, invalidInput("limit must be between 1 and 50")
	}
	if podName != "" && token != "" {
		return nil, invalidInput("continue is invalid with pod_name")
	}
	if s.kubernetes == nil {
		return nil, genargo.MakeKubernetesConfigurationError(errors.New("kubernetes diagnostics are not configured"))
	}
	wf, err := s.lookupWorkflowData(ctx, namespace, name, "")
	if err != nil {
		return nil, mapArgoError(err, argoTarget{action: "get", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
	}
	if wf.UID == "" {
		return nil, genargo.MakeKubernetesResponseError(errors.New("workflow UID is missing"))
	}
	return s.kubernetes.Diagnose(ctx, namespace, name, wf.UID, podName, limit, token)
}
