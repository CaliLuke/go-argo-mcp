package service

import (
	"context"
	"errors"
	"strings"
	"time"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

func (s *ArgoService) WaitWorkflow(ctx context.Context, p *genargo.WaitWorkflowPayload) (*genargo.WaitWorkflowResult, error) {
	namespace, name := s.namespace(p.Namespace), strings.TrimSpace(p.Name)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, invalidInput("name is required")
	}
	if !safeReadSegment(namespace) || !safeReadSegment(name) {
		return nil, invalidInput("namespace and name must be safe path segments")
	}
	duration, interval := p.DurationSeconds, p.PollIntervalSeconds
	if duration == 0 {
		duration = 10
	}
	if interval == 0 {
		interval = 2
	}
	if duration < 1 || duration > 30 || interval < 1 || interval > 5 {
		return nil, invalidInput("wait duration or poll interval is out of range")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(duration)*time.Second)
	defer cancel()
	var latest *genargo.WorkflowDetailResult
	for {
		detail, getErr := client.GetWorkflow(waitCtx, namespace, name)
		if getErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) && latest != nil {
				return &genargo.WaitWorkflowResult{Workflow: latest, TimedOut: true, Source: "argo"}, nil
			}
			return nil, mapArgoError(getErr, argoTarget{action: "wait for", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
		}
		latest = workflowDetailResult(detail)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if waitCtx.Err() != nil {
			return &genargo.WaitWorkflowResult{Workflow: latest, TimedOut: true, Source: "argo"}, nil
		}
		if terminalWorkflowPhase(latest.Status) {
			return &genargo.WaitWorkflowResult{Workflow: latest, Completed: true, Source: "argo"}, nil
		}
		timer := time.NewTimer(time.Duration(interval) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-waitCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return &genargo.WaitWorkflowResult{Workflow: latest, TimedOut: true, Source: "argo"}, nil
		case <-timer.C:
		}
	}
}

func terminalWorkflowPhase(phase string) bool {
	return phase == "Succeeded" || phase == "Failed" || phase == "Error"
}

func workflowDetailResult(detail *argoapi.WorkflowDetail) *genargo.WorkflowDetailResult {
	result := &genargo.WorkflowDetailResult{Name: detail.Summary.Name, Namespace: detail.Summary.Namespace, Status: coalesce(detail.Summary.Status, "Unknown"), Labels: detail.Labels, Annotations: detail.Annotations, Parameters: detail.Parameters, Outputs: detail.Outputs}
	setPtr(&result.Progress, detail.Summary.Progress)
	setPtr(&result.StartedAt, detail.Summary.StartedAt)
	setPtr(&result.FinishedAt, detail.Summary.FinishedAt)
	setPtr(&result.Message, detail.Message)
	if duration := formatDuration(detail.Summary.StartedAt, detail.Summary.FinishedAt); duration != "" {
		result.Duration = &duration
	}
	return result
}
