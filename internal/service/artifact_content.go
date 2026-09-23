package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	loom "github.com/CaliLuke/loom/pkg"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

const maxArtifactOffsetBytes = 16777216

func (s *ArgoService) ReadWorkflowArtifact(ctx context.Context, p *genargo.ReadWorkflowArtifactPayload) (*genargo.ArtifactContentResult, error) {
	namespace := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name, nodeID, artifactName := strings.TrimSpace(p.Name), strings.TrimSpace(p.NodeID), strings.TrimSpace(p.ArtifactName)
	direction := stringDefault(p.Direction, "outputs")
	archiveUID := strings.TrimSpace(pointerValue(p.ArchiveUID))
	maxBytes := p.MaxBytes
	if maxBytes == 0 {
		maxBytes = 65536
	}
	if name == "" || nodeID == "" || artifactName == "" {
		return nil, invalidInput("name, node_id and artifact_name are required")
	}
	if p.OffsetBytes < 0 || p.OffsetBytes > maxArtifactOffsetBytes || maxBytes < 4 || maxBytes > 262144 {
		return nil, invalidInput("artifact byte bounds are out of range")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if archiveUID == "" {
		_, err = client.ArtifactURL(namespace, name, nodeID, direction, artifactName)
	} else {
		_, err = client.ArchivedArtifactURL(namespace, archiveUID, nodeID, direction, artifactName)
	}
	if err != nil {
		return nil, invalidInput(err.Error())
	}
	wf, err := s.lookupWorkflowData(ctx, namespace, name, archiveUID)
	if err != nil {
		return nil, mapRetainedWorkflowError(err, "read artifact metadata for", namespace, name, archiveUID)
	}
	var artifactsFound bool
	var nodeFound bool
	for _, node := range wf.Nodes {
		if node.ID != nodeID {
			continue
		}
		nodeFound = true
		artifacts := node.Outputs
		if direction == "inputs" {
			artifacts = node.Inputs
		}
		for _, artifact := range artifacts {
			if artifact.Name == artifactName {
				artifactsFound = true
				break
			}
		}
		break
	}
	if !nodeFound {
		return nil, invalidInput(fmt.Sprintf("workflow node %q was not found", nodeID))
	}
	if !artifactsFound {
		return nil, invalidInput(fmt.Sprintf("artifact %q is not declared on node %q in %s", artifactName, nodeID, direction))
	}
	content, err := client.ReadArtifactContent(ctx, namespace, name, archiveUID, nodeID, direction, artifactName, p.OffsetBytes, maxBytes)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "read artifact for", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
	}
	if content.HasMore && content.NextOffset > maxArtifactOffsetBytes {
		return nil, invalidInput("artifact continuation exceeds the supported offset_bytes maximum; use get_workflow_artifacts and its download_url for larger content")
	}
	result := &genargo.ArtifactContentResult{
		Namespace: namespace, Name: name, NodeID: nodeID, ArtifactName: artifactName, Direction: direction,
		Text: content.Text, OffsetBytes: content.OffsetBytes, ReturnedBytes: content.ReturnedBytes,
		HasMore: content.HasMore, Source: "argo",
	}
	if archiveUID != "" {
		result.ArchiveUID = &archiveUID
	}
	if content.HasMore {
		result.NextOffset = &content.NextOffset
		result.Note = strPtr("More artifact text remains; call again with next_offset.")
	}
	return result, nil
}

func mapRetainedWorkflowError(err error, action, namespace, name, archiveUID string) error {
	var serviceErr *loom.ServiceError
	if errors.As(err, &serviceErr) {
		return err
	}
	var httpErr *argoapi.HTTPError
	if archiveUID == "" && errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
		return loom.WithErrorRemedy(genargo.MakeArgoNotFound(err), &loom.ErrorRemedy{
			Code:        "argo.resource.not_found",
			SafeMessage: fmt.Sprintf("Workflow %q in namespace %q was not found.", name, namespace),
			RetryHint:   "Call list_archived_workflows to discover a retained workflow UID, then retry with archive_uid. No implicit archive lookup is performed.",
		})
	}
	return mapNewArgoError(err, argoTarget{action: action, resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
}
