package argoapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/CaliLuke/go-argo-mcp/internal/argoapi/models"
)

type WorkflowNode struct {
	ID, Name, DisplayName, Type, Phase, TemplateName, BoundaryID string
	Children                                                     []string
	StartedAt, FinishedAt, Message                               string
	Inputs, Outputs                                              []Artifact
	InputParameters, OutputParameters                            map[string]string
	OutputResult, ExitCode                                       string
}

type Artifact struct {
	Name, Path string
	Optional   bool
}

type WorkflowData struct {
	UID    string
	Detail WorkflowDetail
	Nodes  []WorkflowNode
}

type ArchivedWorkflow struct {
	UID    string
	Detail WorkflowDetail
}

func (c *Client) GetWorkflowData(ctx context.Context, namespace, name string) (*WorkflowData, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
		return nil, err
	}
	return workflowDataFromModel(resp), nil
}

func (c *Client) GetArchivedWorkflowData(ctx context.Context, namespace, name, archiveUID string) (*WorkflowData, error) {
	endpoint := c.baseURL + "/api/v1/archived-workflows/" + url.PathEscape(archiveUID)
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodGet, endpoint, map[string]string{"namespace": namespace}, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Metadata.Namespace != namespace || resp.Metadata.Name != name || resp.Metadata.UID != archiveUID {
		return nil, fmt.Errorf("archived workflow identity mismatch")
	}
	return workflowDataFromModel(resp), nil
}

func workflowDataFromModel(resp models.Workflow) *WorkflowData {
	nodes := make([]WorkflowNode, 0, len(resp.Status.Nodes))
	for key, n := range resp.Status.Nodes {
		id := n.ID
		if id == "" {
			id = key
		}
		nodes = append(nodes, WorkflowNode{
			ID: id, Name: n.Name, DisplayName: n.DisplayName, Type: n.Type, Phase: n.Phase,
			TemplateName: n.TemplateName, BoundaryID: n.BoundaryID, Children: append([]string(nil), n.Children...),
			StartedAt: n.StartedAt, FinishedAt: n.FinishedAt, Message: n.Message,
			Inputs: artifacts(n.Inputs.Artifacts), Outputs: artifacts(n.Outputs.Artifacts),
			InputParameters: renderParameters(n.Inputs.Parameters), OutputParameters: renderParameters(n.Outputs.Parameters),
			OutputResult: n.Outputs.Result, ExitCode: n.Outputs.ExitCode,
		})
	}
	detail := WorkflowDetail{Summary: workflowSummaryFromModel(resp), Message: resp.Status.Message, Labels: nonnilStringMap(resp.Metadata.Labels), Annotations: nonnilStringMap(resp.Metadata.Annotations), Parameters: renderParameters(resp.Spec.Arguments.Parameters), Outputs: renderParameters(resp.Status.Outputs.Parameters)}
	return &WorkflowData{UID: resp.Metadata.UID, Detail: detail, Nodes: nodes}
}

func artifacts(in []models.Artifact) []Artifact {
	out := make([]Artifact, 0, len(in))
	for _, a := range in {
		out = append(out, Artifact{Name: a.Name, Path: a.Path, Optional: a.Optional})
	}
	return out
}

func (c *Client) ListArchivedWorkflows(ctx context.Context, namespace, labelSelector, namePrefix string, limit int, continueToken string) (Page[ArchivedWorkflow], error) {
	limit, err := boundedLimit(limit, defaultCollectionLimit)
	if err != nil {
		return Page[ArchivedWorkflow]{}, err
	}
	endpoint := c.baseURL + "/api/v1/archived-workflows"
	query := map[string]string{"namespace": namespace, "namePrefix": namePrefix, "listOptions.labelSelector": labelSelector, "listOptions.limit": fmt.Sprint(limit), "listOptions.continue": continueToken}
	var resp models.WorkflowList
	if err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil, &resp); err != nil {
		return Page[ArchivedWorkflow]{}, err
	}
	if len(resp.Items) > limit {
		return Page[ArchivedWorkflow]{}, fmt.Errorf("archive page returned %d items for limit %d and cannot continue without dropping items", len(resp.Items), limit)
	}
	out := make([]ArchivedWorkflow, 0, len(resp.Items))
	for _, item := range resp.Items {
		data := workflowDataFromModel(item)
		out = append(out, ArchivedWorkflow{UID: data.UID, Detail: data.Detail})
	}
	return Page[ArchivedWorkflow]{Items: out, Continue: resp.Metadata.Continue}, nil
}

func (c *Client) GetArchivedWorkflow(ctx context.Context, namespace, uid string) (*ArchivedWorkflow, error) {
	endpoint := c.baseURL + "/api/v1/archived-workflows/" + url.PathEscape(uid)
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodGet, endpoint, map[string]string{"namespace": namespace}, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Metadata.Namespace != namespace {
		return nil, fmt.Errorf("archived workflow namespace mismatch")
	}
	data := workflowDataFromModel(resp)
	return &ArchivedWorkflow{UID: data.UID, Detail: data.Detail}, nil
}

func sortedParameters(parameters map[string]string) []string {
	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+parameters[k])
	}
	return out
}

func (c *Client) SubmitWorkflow(ctx context.Context, namespace, kind, name string, parameters map[string]string) (*WorkflowSummary, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/submit"
	body := models.WorkflowSubmitRequest{Namespace: namespace, ResourceKind: kind, ResourceName: name, SubmitOptions: models.SubmitOpts{Parameters: sortedParameters(parameters)}}
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, body, &resp); err != nil {
		return nil, err
	}
	summary := workflowSummaryFromModel(resp)
	return &summary, nil
}

func (c *Client) ResubmitWorkflow(ctx context.Context, namespace, name string, memoized bool, parameters map[string]string) (*WorkflowSummary, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/resubmit"
	body := models.WorkflowResubmitRequest{Name: name, Namespace: namespace, Memoized: memoized, Parameters: sortedParameters(parameters)}
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodPut, endpoint, nil, body, &resp); err != nil {
		return nil, err
	}
	summary := workflowSummaryFromModel(resp)
	return &summary, nil
}

func (c *Client) SetWorkflowSuspended(ctx context.Context, namespace, name string, suspended bool) error {
	action := "resume"
	var body any = models.WorkflowResumeRequest{Name: name, Namespace: namespace}
	if suspended {
		action = "suspend"
		body = models.WorkflowSuspendRequest{Name: name, Namespace: namespace}
	}
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/" + action
	return c.doJSON(ctx, http.MethodPut, endpoint, nil, body, nil)
}

func (c *Client) LintWorkflow(ctx context.Context, namespace string, manifest json.RawMessage) (*WorkflowSummary, error) {
	endpoint := c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/lint"
	var resp models.Workflow
	if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, models.WorkflowLintRequest{Namespace: namespace, Workflow: manifest}, &resp); err != nil {
		return nil, err
	}
	summary := workflowSummaryFromModel(resp)
	return &summary, nil
}

func (c *Client) LintWorkflowTemplate(ctx context.Context, namespace string, manifest json.RawMessage, cluster bool) (string, error) {
	if cluster {
		endpoint := c.baseURL + "/api/v1/cluster-workflow-templates/lint"
		var resp models.ClusterWorkflowTemplate
		if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, models.ClusterWorkflowTemplateLintRequest{Template: manifest}, &resp); err != nil {
			return "", err
		}
		return resp.Metadata.Name, nil
	}
	endpoint := c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace) + "/lint"
	var resp models.WorkflowTemplate
	if err := c.doJSON(ctx, http.MethodPost, endpoint, nil, models.WorkflowTemplateLintRequest{Namespace: namespace, Template: manifest}, &resp); err != nil {
		return "", err
	}
	return resp.Metadata.Name, nil
}

func (c *Client) ArtifactURL(namespace, workflow, nodeID, direction, artifact string) (string, error) {
	return c.artifactURL(namespace, "workflows", workflow, nodeID, direction, artifact)
}

func (c *Client) ArchivedArtifactURL(namespace, archiveUID, nodeID, direction, artifact string) (string, error) {
	return c.artifactURL(namespace, "archived-workflows", archiveUID, nodeID, direction, artifact)
}

func (c *Client) artifactURL(namespace, collection, workflowIdentity, nodeID, direction, artifact string) (string, error) {
	for _, value := range []string{namespace, workflowIdentity, nodeID, artifact} {
		if !safeArtifactSegment(value) {
			return "", fmt.Errorf("invalid artifact link identity")
		}
	}
	if direction != "inputs" && direction != "outputs" {
		return "", fmt.Errorf("invalid artifact direction")
	}
	return c.baseURL + "/artifact-files/" + url.PathEscape(namespace) + "/" + collection + "/" + url.PathEscape(workflowIdentity) + "/" + url.PathEscape(nodeID) + "/" + direction + "/" + url.PathEscape(artifact), nil
}

func safeArtifactSegment(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 4096 {
		return false
	}
	for range 3 {
		if value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
			return false
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return false
		}
		if decoded == value {
			return true
		}
		value = decoded
	}
	return false
}

func ValidatePathSegment(value string) error {
	if !safeArtifactSegment(value) {
		return fmt.Errorf("invalid path identity")
	}
	return nil
}
