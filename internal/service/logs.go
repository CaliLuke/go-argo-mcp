package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

const (
	defaultLogMaxBytes = 1 << 20
	maximumLogMaxBytes = 4 << 20
	maxArchiveLogNodes = 20
)

func (s *ArgoService) GetWorkflowLogs(ctx context.Context, p *genargo.GetWorkflowLogsPayload) (*genargo.WorkflowLogsResult, error) {
	namespace := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(namespace); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.WorkflowName)
	container := stringDefault(p.Container, "main")
	source := stringDefault(p.Source, "auto")
	podName, nodeID, archiveUID := strings.TrimSpace(pointerValue(p.PodName)), strings.TrimSpace(pointerValue(p.NodeID)), strings.TrimSpace(pointerValue(p.ArchiveUID))
	maxBytes := p.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultLogMaxBytes
	}
	if name == "" {
		return nil, invalidInput("workflow_name is required")
	}
	if source != "auto" && source != "live" && source != "archive" {
		return nil, invalidInput("source must be auto, live or archive")
	}
	if maxBytes < 1024 || maxBytes > maximumLogMaxBytes {
		return nil, invalidInput("max_bytes must be between 1024 and 4194304")
	}
	if p.MaxLines < 0 {
		return nil, invalidInput("max_lines cannot be negative")
	}
	if podName != "" && nodeID != "" {
		return nil, invalidInput("pod_name and node_id cannot be combined")
	}
	if source == "live" && (archiveUID != "" || nodeID != "") {
		return nil, invalidInput("source live cannot be combined with archive_uid or node_id")
	}
	if (source == "archive" || archiveUID != "" || nodeID != "") && podName != "" {
		return nil, invalidInput("archive log reads cannot use pod_name; replace pod_name with node_id")
	}
	for _, segment := range []string{namespace, name, container} {
		if err := argoapi.ValidatePathSegment(segment); err != nil {
			return nil, invalidInput(err.Error())
		}
	}
	if podName != "" {
		if err := argoapi.ValidatePathSegment(podName); err != nil {
			return nil, invalidInput(err.Error())
		}
	}
	if nodeID != "" {
		if err := argoapi.ValidatePathSegment(nodeID); err != nil {
			return nil, invalidInput(err.Error())
		}
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}

	useArchive := source == "archive" || archiveUID != "" || nodeID != ""
	fallbackNote := ""
	if !useArchive {
		entries, truncated, liveErr := client.GetWorkflowLogs(ctx, namespace, name, podName, container, maxBytes)
		if liveErr == nil && (len(entries) > 0 || source == "live") {
			return buildWorkflowLogsResult(namespace, name, container, podName, stringPtrValue(p.Search), p.MaxLines, maxBytes, entries, "live", truncated, liveLogNote(name, namespace, container, entries, truncated))
		}
		if liveErr != nil {
			var httpErr *argoapi.HTTPError
			if source == "live" || !errors.As(liveErr, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
				return nil, mapArgoError(liveErr, argoTarget{action: "get logs for", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
			}
			fallbackNote = "Live logs were not found; inspected retained log artifacts."
		} else {
			fallbackNote = "Live logs were empty; inspected retained log artifacts."
		}
		if podName != "" {
			return nil, invalidInput("retained log fallback cannot preserve pod_name; replace pod_name with node_id")
		}
	}

	entries, truncated, archiveNote, err := s.readArchiveLogs(ctx, client, namespace, name, archiveUID, nodeID, container, maxBytes)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(strings.Join([]string{fallbackNote, archiveNote}, " "))
	return buildWorkflowLogsResult(namespace, name, container, "", stringPtrValue(p.Search), p.MaxLines, maxBytes, entries, "archive", truncated, note)
}

func (s *ArgoService) readArchiveLogs(ctx context.Context, client *argoapi.Client, namespace, name, archiveUID, nodeID, container string, maxBytes int) ([]argoapi.WorkflowLogEntry, bool, string, error) {
	if archiveUID == "" {
		if _, err := client.ArtifactURL(namespace, name, "validation", "outputs", container+"-logs"); err != nil {
			return nil, false, "", invalidInput(err.Error())
		}
	} else if _, err := client.ArchivedArtifactURL(namespace, archiveUID, "validation", "outputs", container+"-logs"); err != nil {
		return nil, false, "", invalidInput(err.Error())
	}
	wf, err := s.lookupWorkflowData(ctx, namespace, name, archiveUID)
	if err != nil {
		return nil, false, "", mapRetainedWorkflowError(err, "inspect retained logs for", namespace, name, archiveUID)
	}
	wanted := container + "-logs"
	nodes := append([]argoapi.WorkflowNode(nil), wf.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	if nodeID != "" {
		found := false
		for _, node := range nodes {
			if node.ID == nodeID {
				nodes, found = []argoapi.WorkflowNode{node}, true
				break
			}
		}
		if !found {
			return nil, false, "", invalidInput(fmt.Sprintf("workflow node %q was not found", nodeID))
		}
	}
	matching := nodes[:0]
	for _, node := range nodes {
		for _, artifact := range node.Outputs {
			if artifact.Name == wanted {
				matching = append(matching, node)
				break
			}
		}
	}
	if len(matching) == 0 {
		return []argoapi.WorkflowLogEntry{}, false, fmt.Sprintf("No declared output artifact named %q was found; retained workflow metadata and log retention are distinct.", wanted), nil
	}
	truncated := len(matching) > maxArchiveLogNodes
	if len(matching) > maxArchiveLogNodes {
		matching = matching[:maxArchiveLogNodes]
	}
	entries := []argoapi.WorkflowLogEntry{}
	remaining := maxBytes
	for index, node := range matching {
		if remaining < utf8.UTFMax {
			truncated = true
			break
		}
		content, readErr := client.ReadArtifactContent(ctx, namespace, name, archiveUID, node.ID, "outputs", wanted, 0, remaining)
		if readErr != nil {
			return nil, false, "", mapNewArgoError(readErr, argoTarget{action: "read retained logs for", resource: "Workflow", namespace: namespace, name: name, listTool: "list_workflows"})
		}
		remaining -= content.ReturnedBytes
		truncated = truncated || content.HasMore || remaining == 0 && index+1 < len(matching)
		pod := node.Name
		if pod == "" {
			pod = node.ID
		}
		for _, line := range strings.Split(content.Text, "\n") {
			if line != "" {
				entries = append(entries, argoapi.WorkflowLogEntry{PodName: pod, Content: line})
			}
		}
	}
	note := "Read retained log artifacts declared by the workflow."
	if truncated {
		note = "Retained logs are a bounded prefix; increase max_bytes or select node_id to inspect a narrower result."
	}
	return entries, truncated, note, nil
}

func buildWorkflowLogsResult(namespace, name, container, podName, search string, maxLines, maxBytes int, entries []argoapi.WorkflowLogEntry, source string, truncated bool, note string) (*genargo.WorkflowLogsResult, error) {
	total := len(entries)
	filtered := entries
	if search != "" {
		filtered = filterLogs(entries, search)
	}
	matching := len(filtered)
	if maxLines > 0 && len(filtered) > maxLines {
		filtered = filtered[len(filtered)-maxLines:]
	}
	rendered := renderLogs(filtered)
	if len(rendered) > maxBytes {
		end := maxBytes
		for end > 0 && !utf8.ValidString(rendered[:end]) {
			end--
		}
		rendered = rendered[:end]
		truncated = true
		note = strings.TrimSpace(note + " Rendered logs were capped at max_bytes on a UTF-8 boundary.")
	}
	if !utf8.ValidString(rendered) {
		return nil, fmt.Errorf("rendered logs are not valid UTF-8")
	}
	res := &genargo.WorkflowLogsResult{Namespace: namespace, Workflow: name, Container: container, TotalLines: total, MatchingLines: matching, ReturnedLines: len(filtered), Logs: rendered, Source: source, Truncated: truncated}
	if podName != "" {
		res.Pod = &podName
	}
	if search != "" {
		res.SearchTerm = &search
	}
	if maxLines > 0 {
		res.MaxLines = &maxLines
	}
	if search != "" && matching == 0 {
		note = strings.TrimSpace(note + " " + fmt.Sprintf("No log entries matched %q among the %d entries returned by Argo.", search, total))
	} else if matching > len(filtered) {
		note = strings.TrimSpace(note + " " + fmt.Sprintf("Showing last %d of %d matching lines.", len(filtered), matching))
	}
	if note != "" {
		res.Note = &note
	}
	return res, nil
}

func liveLogNote(name, namespace, container string, entries []argoapi.WorkflowLogEntry, truncated bool) string {
	if truncated {
		return "Live logs are a bounded prefix; counts and tail filtering apply only within collected content."
	}
	if len(entries) == 0 {
		return fmt.Sprintf("Argo returned no log entries for Workflow %q in namespace %q and container %q. The pods may have produced no logs, may have been removed, or logs may no longer be retained.", name, namespace, container)
	}
	return ""
}
