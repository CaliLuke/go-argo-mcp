package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/argoapi"
)

const maxManifestBytes = 256 << 10

func (s *ArgoService) GetWorkflowNodes(ctx context.Context, p *genargo.GetWorkflowNodesPayload) (*genargo.WorkflowNodesResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, invalidInput("name is required")
	}
	limit, err := boundedDiagnostic(p.Offset, p.Limit)
	if err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	wf, err := c.GetWorkflowData(ctx, ns, p.Name)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "get nodes for", resource: "Workflow", namespace: ns, name: p.Name, listTool: "list_workflows"})
	}
	phase, nodeID := pointerValue(p.Phase), pointerValue(p.NodeID)
	nodes := append([]argoapi.WorkflowNode(nil), wf.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	filtered := nodes[:0]
	for _, n := range nodes {
		if (phase == "" || n.Phase == phase) && (nodeID == "" || n.ID == nodeID) {
			filtered = append(filtered, n)
		}
	}
	total := len(filtered)
	page, more := slicePage(filtered, p.Offset, limit)
	out := make([]*genargo.WorkflowNodeSummary, 0, len(page))
	fields := false
	for _, n := range page {
		id, ti := truncateUTF8(n.ID, 256)
		name, tn := truncateUTF8(n.Name, 1024)
		typ, tt := truncateUTF8(n.Type, 256)
		fields = fields || ti || tn || tt
		children := append(make([]string, 0, len(n.Children)), n.Children...)
		if len(children) > 200 {
			children = children[:200]
			fields = true
		}
		for i := range children {
			var tr bool
			children[i], tr = truncateUTF8(children[i], 256)
			fields = fields || tr
		}
		r := &genargo.WorkflowNodeSummary{ID: id, Name: name, Type: typ, Children: children}
		setString(&r.DisplayName, n.DisplayName, 1024, &fields)
		setString(&r.Phase, n.Phase, 256, &fields)
		setString(&r.TemplateName, n.TemplateName, 1024, &fields)
		setString(&r.BoundaryID, n.BoundaryID, 256, &fields)
		setString(&r.StartedAt, n.StartedAt, 256, &fields)
		setString(&r.FinishedAt, n.FinishedAt, 256, &fields)
		setString(&r.Message, n.Message, 4096, &fields)
		out = append(out, r)
	}
	res := &genargo.WorkflowNodesResult{Nodes: out, Total: total, Count: len(out), Truncated: more || fields, FieldsTruncated: fields}
	if more {
		next := p.Offset + len(out)
		res.NextOffset = &next
	}
	if res.Truncated {
		res.Note = strPtr("Results are bounded; truncated indicates another page or shortened diagnostic fields.")
	}
	return res, nil
}

func (s *ArgoService) GetWorkflowArtifacts(ctx context.Context, p *genargo.GetWorkflowArtifactsPayload) (*genargo.WorkflowArtifactsResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, invalidInput("name is required")
	}
	limit, err := boundedDiagnostic(p.Offset, p.Limit)
	if err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	wf, err := c.GetWorkflowData(ctx, ns, p.Name)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "get artifacts for", resource: "Workflow", namespace: ns, name: p.Name, listTool: "list_workflows"})
	}
	type item struct {
		node, direction string
		artifact        argoapi.Artifact
	}
	items := []item{}
	filter := pointerValue(p.NodeID)
	for _, n := range wf.Nodes {
		if filter != "" && n.ID != filter {
			continue
		}
		for _, a := range n.Inputs {
			items = append(items, item{n.ID, "inputs", a})
		}
		for _, a := range n.Outputs {
			items = append(items, item{n.ID, "outputs", a})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].node != items[j].node {
			return items[i].node < items[j].node
		}
		if items[i].direction != items[j].direction {
			return items[i].direction < items[j].direction
		}
		return items[i].artifact.Name < items[j].artifact.Name
	})
	total := len(items)
	page, more := slicePage(items, p.Offset, limit)
	out := make([]*genargo.WorkflowArtifactSummary, 0, len(page))
	fields := false
	for _, it := range page {
		node, tn := truncateUTF8(it.node, 256)
		name, ta := truncateUTF8(it.artifact.Name, 256)
		path, tp := truncateUTF8(it.artifact.Path, 1024)
		fields = fields || tn || ta || tp
		r := &genargo.WorkflowArtifactSummary{Name: name, NodeID: node, Direction: it.direction, Optional: it.artifact.Optional}
		if path != "" {
			r.Path = &path
		}
		link, e := c.ArtifactURL(ns, p.Name, it.node, it.direction, it.artifact.Name)
		if e == nil && len(link) <= 4096 {
			r.DownloadURL = &link
		} else {
			fields = true
		}
		out = append(out, r)
	}
	res := &genargo.WorkflowArtifactsResult{Artifacts: out, Total: total, Count: len(out), Truncated: more || fields, FieldsTruncated: fields}
	if more {
		next := p.Offset + len(out)
		res.NextOffset = &next
	}
	if res.Truncated {
		res.Note = strPtr("Results are bounded; truncated indicates another page or shortened fields or omitted links.")
	}
	return res, nil
}

func (s *ArgoService) GetWorkflowEvents(ctx context.Context, p *genargo.GetWorkflowEventsPayload) (*genargo.WorkflowEventsResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, invalidInput("name is required")
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	duration := p.DurationSeconds
	if duration == 0 {
		duration = 2
	}
	if limit < 1 || limit > 200 || duration < 1 || duration > 10 {
		return nil, invalidInput("event limit or duration is out of range")
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	obs, err := c.ObserveWorkflowEvents(ctx, ns, p.Name, limit, time.Duration(duration)*time.Second)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "observe events for", resource: "Workflow", namespace: ns, name: p.Name, listTool: "list_workflows"})
	}
	events := make([]*genargo.WorkflowEventSummary, 0, len(obs.Events))
	fields := false
	for _, e := range obs.Events {
		msg, tr := truncateUTF8(e.Message, 4096)
		fields = fields || tr
		r := &genargo.WorkflowEventSummary{Type: e.Type, Count: e.Count}
		setPtr(&r.Reason, e.Reason)
		setPtr(&r.Message, msg)
		setPtr(&r.FirstTimestamp, e.FirstTimestamp)
		setPtr(&r.LastTimestamp, e.LastTimestamp)
		setPtr(&r.EventTime, e.EventTime)
		events = append(events, r)
	}
	note := "Observed live events for the bounded collection window; this is not historical event listing."
	if obs.LimitReached {
		note = "Collection stopped because the requested event limit was reached."
	}
	return &genargo.WorkflowEventsResult{Events: events, Count: len(events), LimitReached: obs.LimitReached, FieldsTruncated: fields, Note: note}, nil
}

func (s *ArgoService) ListArchivedWorkflows(ctx context.Context, p *genargo.ListArchivedWorkflowsPayload) (*genargo.ListArchivedWorkflowsResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit == 0 {
		limit = 50
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	page, err := c.ListArchivedWorkflows(ctx, ns, pointerValue(p.LabelSelector), pointerValue(p.NamePrefix), limit, pointerValue(p.Continue))
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "list", resource: "ArchivedWorkflows", namespace: ns})
	}
	items := make([]*genargo.ArchivedWorkflowSummary, 0, len(page.Items))
	for _, x := range page.Items {
		d := x.Detail
		items = append(items, &genargo.ArchivedWorkflowSummary{UID: x.UID, Name: d.Summary.Name, Namespace: d.Summary.Namespace, Status: d.Summary.Status, StartedAt: optional(d.Summary.StartedAt), FinishedAt: optional(d.Summary.FinishedAt)})
	}
	res := &genargo.ListArchivedWorkflowsResult{Workflows: items, Count: len(items), HasMore: page.Continue != ""}
	if page.Continue != "" {
		res.Continue = &page.Continue
	}
	return res, nil
}

func (s *ArgoService) GetArchivedWorkflow(ctx context.Context, p *genargo.GetArchivedWorkflowPayload) (*genargo.ArchivedWorkflowDetailResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.UID) == "" {
		return nil, invalidInput("uid is required")
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	x, err := c.GetArchivedWorkflow(ctx, ns, p.UID)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "get", resource: "ArchivedWorkflow", namespace: ns, name: p.UID, listTool: "list_archived_workflows"})
	}
	d := x.Detail
	message, tr := truncateUTF8(d.Message, 4096)
	labels, t1 := boundedMap(d.Labels)
	annotations, t2 := boundedMap(d.Annotations)
	parameters, t3 := boundedMap(d.Parameters)
	outputs, t4 := boundedMap(d.Outputs)
	res := &genargo.ArchivedWorkflowDetailResult{UID: x.UID, Name: d.Summary.Name, Namespace: d.Summary.Namespace, Status: d.Summary.Status, Labels: labels, Annotations: annotations, Parameters: parameters, Outputs: outputs, Truncated: tr || t1 || t2 || t3 || t4, StartedAt: optional(d.Summary.StartedAt), FinishedAt: optional(d.Summary.FinishedAt)}
	if message != "" {
		res.Message = &message
	}
	return res, nil
}

func (s *ArgoService) LintWorkflow(ctx context.Context, p *genargo.LintWorkflowPayload) (*genargo.LintResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	manifest, name, err := validateManifest(p.ManifestJSON, "Workflow", ns, false)
	if err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	summary, err := c.LintWorkflow(ctx, ns, manifest)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "lint", resource: "Workflow", namespace: ns, name: name})
	}
	if summary.Name != "" {
		name = summary.Name
	}
	return &genargo.LintResult{Valid: true, Name: name, Namespace: &ns, Scope: "namespaced", Source: "argo"}, nil
}

func (s *ArgoService) LintWorkflowTemplate(ctx context.Context, p *genargo.LintWorkflowTemplatePayload) (*genargo.LintResult, error) {
	cluster := p.ClusterScope
	if cluster && p.Namespace != nil {
		return nil, invalidInput("namespace is forbidden for cluster-scoped templates")
	}
	ns := ""
	kind := "ClusterWorkflowTemplate"
	if !cluster {
		ns = s.namespace(p.Namespace)
		if err := s.authorizeNamespace(ns); err != nil {
			return nil, err
		}
		kind = "WorkflowTemplate"
	}
	manifest, name, err := validateManifest(p.ManifestJSON, kind, ns, cluster)
	if err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	got, err := c.LintWorkflowTemplate(ctx, ns, manifest, cluster)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "lint", resource: kind, namespace: ns, name: name})
	}
	if got != "" {
		name = got
	}
	scope := "namespaced"
	if cluster {
		scope = "cluster"
	}
	res := &genargo.LintResult{Valid: true, Name: name, Scope: scope, Source: "argo"}
	if ns != "" {
		res.Namespace = &ns
	}
	return res, nil
}

func (s *ArgoService) SubmitWorkflowTemplate(ctx context.Context, p *genargo.SubmitWorkflowTemplatePayload) (*genargo.CreatedWorkflowResult, error) {
	kind := "WorkflowTemplate"
	if p.ClusterScope {
		kind = "ClusterWorkflowTemplate"
	}
	return s.submit(ctx, p.Namespace, p.TemplateName, kind, p.Parameters)
}
func (s *ArgoService) TriggerCronWorkflow(ctx context.Context, p *genargo.TriggerCronWorkflowPayload) (*genargo.CreatedWorkflowResult, error) {
	return s.submit(ctx, p.Namespace, p.Name, "CronWorkflow", p.Parameters)
}
func (s *ArgoService) submit(ctx context.Context, nsPtr *string, name, kind string, params map[string]string) (*genargo.CreatedWorkflowResult, error) {
	ns := s.namespace(nsPtr)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if !s.policy.AllowMutations {
		return nilOrDenied(kind, ns, name), nil
	}
	if err := validateNameAndParameters(name, params); err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	summary, err := c.SubmitWorkflow(ctx, ns, kind, name, params)
	if err != nil {
		return nil, mapMutationError(err, argoTarget{action: "submit", resource: kind, namespace: ns, name: name})
	}
	return created(summary, fmt.Sprintf("%s %q was submitted as Workflow %q.", kind, name, summary.Name)), nil
}

func (s *ArgoService) SuspendWorkflow(ctx context.Context, p *genargo.SuspendWorkflowPayload) (*genargo.ActionResult, error) {
	return s.setSuspended(ctx, p.Namespace, p.Name, true)
}
func (s *ArgoService) ResumeWorkflow(ctx context.Context, p *genargo.ResumeWorkflowPayload) (*genargo.ActionResult, error) {
	return s.setSuspended(ctx, p.Namespace, p.Name, false)
}
func (s *ArgoService) setSuspended(ctx context.Context, nsPtr *string, name string, suspended bool) (*genargo.ActionResult, error) {
	ns := s.namespace(nsPtr)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalidInput("name is required")
	}
	action := "resume"
	completedAction := "resumed"
	if suspended {
		action = "suspend"
		completedAction = "suspended"
	}
	if !s.policy.AllowMutations {
		return deniedActionResult(action+"_workflow", "MCP_ALLOW_MUTATIONS", ns, name), nil
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	if err := c.SetWorkflowSuspended(ctx, ns, name, suspended); err != nil {
		return nil, mapMutationError(err, argoTarget{action: action, resource: "Workflow", namespace: ns, name: name, listTool: "list_workflows"})
	}
	return actionResult("ok", fmt.Sprintf("Workflow %q in namespace %q was %s.", name, ns, completedAction), ns, name), nil
}

func (s *ArgoService) ResubmitWorkflow(ctx context.Context, p *genargo.ResubmitWorkflowPayload) (*genargo.CreatedWorkflowResult, error) {
	ns := s.namespace(p.Namespace)
	if err := s.authorizeNamespace(ns); err != nil {
		return nil, err
	}
	if !s.policy.AllowMutations {
		return nilOrDenied("resubmit_workflow", ns, p.Name), nil
	}
	if err := validateNameAndParameters(p.Name, p.Parameters); err != nil {
		return nil, err
	}
	c, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	summary, err := c.ResubmitWorkflow(ctx, ns, p.Name, p.Memoized, p.Parameters)
	if err != nil {
		return nil, mapMutationError(err, argoTarget{action: "resubmit", resource: "Workflow", namespace: ns, name: p.Name, listTool: "list_workflows"})
	}
	return created(summary, fmt.Sprintf("Workflow %q was resubmitted as Workflow %q.", p.Name, summary.Name)), nil
}

func boundedDiagnostic(offset, limit int) (int, error) {
	if offset < 0 {
		return 0, invalidInput("offset must be non-negative")
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return 0, invalidInput("limit must be between 1 and 200")
	}
	return limit, nil
}
func slicePage[T any](items []T, offset, limit int) ([]T, bool) {
	if offset >= len(items) {
		return []T{}, false
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], end < len(items)
}
func truncateUTF8(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
func setString(dst **string, value string, max int, truncated *bool) {
	v, tr := truncateUTF8(value, max)
	*truncated = *truncated || tr
	if v != "" {
		*dst = &v
	}
}
func setPtr(dst **string, value string) {
	if value != "" {
		*dst = &value
	}
}
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func boundedMap(in map[string]string) (map[string]string, bool) {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tr := len(keys) > 20
	if len(keys) > 20 {
		keys = keys[:20]
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		tk, a := truncateUTF8(k, 256)
		tv, b := truncateUTF8(in[k], 1024)
		tr = tr || a || b
		out[tk] = tv
	}
	return out, tr
}
func validateManifest(text, kind, namespace string, cluster bool) (json.RawMessage, string, error) {
	if len([]byte(text)) > maxManifestBytes {
		return nil, "", invalidInput("manifest_json exceeds 256 KiB")
	}
	dec := json.NewDecoder(strings.NewReader(text))
	var root map[string]json.RawMessage
	if err := dec.Decode(&root); err != nil {
		return nil, "", invalidInput("manifest_json must contain one JSON object")
	}
	if root == nil {
		return nil, "", invalidInput("manifest_json must contain one JSON object")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, "", invalidInput("manifest_json contains trailing JSON")
	}
	var api, gotKind string
	_ = json.Unmarshal(root["apiVersion"], &api)
	_ = json.Unmarshal(root["kind"], &gotKind)
	if api != "argoproj.io/v1alpha1" || gotKind != kind {
		return nil, "", invalidInput("manifest apiVersion or kind is invalid")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(root["metadata"], &metadata); err != nil {
		return nil, "", invalidInput("manifest metadata must be an object")
	}
	if metadata == nil {
		return nil, "", invalidInput("manifest metadata must be an object")
	}
	name, namePresent, err := manifestString(metadata, "name")
	if err != nil {
		return nil, "", err
	}
	generateName, generatePresent, err := manifestString(metadata, "generateName")
	if err != nil {
		return nil, "", err
	}
	if kind == "Workflow" {
		if (!namePresent || strings.TrimSpace(name) == "") && (!generatePresent || strings.TrimSpace(generateName) == "") {
			return nil, "", invalidInput("workflow metadata.name or metadata.generateName is required")
		}
	} else if !namePresent || strings.TrimSpace(name) == "" {
		return nil, "", invalidInput("manifest metadata.name is required")
	}
	manifestNS, namespacePresent, err := manifestString(metadata, "namespace")
	if err != nil {
		return nil, "", err
	}
	if cluster && namespacePresent {
		return nil, "", invalidInput("cluster template metadata.namespace is forbidden")
	}
	if !cluster {
		if namespacePresent && manifestNS != namespace {
			return nil, "", invalidInput("manifest namespace does not match resolved namespace")
		}
		if !namespacePresent {
			raw, _ := json.Marshal(namespace)
			metadata["namespace"] = raw
			root["metadata"], _ = json.Marshal(metadata)
		}
	}
	data, err := json.Marshal(root)
	if err != nil {
		return nil, "", invalidInput("manifest cannot be encoded")
	}
	if name != "" {
		return data, name, nil
	}
	return data, generateName, nil
}

func manifestString(metadata map[string]json.RawMessage, key string) (string, bool, error) {
	raw, present := metadata[key]
	if !present {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, invalidInput("manifest metadata." + key + " must be a string")
	}
	return value, true, nil
}
func validateNameAndParameters(name string, params map[string]string) error {
	if strings.TrimSpace(name) == "" {
		return invalidInput("name is required")
	}
	if len(params) > 128 {
		return invalidInput("parameters exceed 128 entries")
	}
	size := 0
	for k, v := range params {
		if k == "" || strings.Contains(k, "=") || strings.IndexFunc(k, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return invalidInput("parameter name is invalid")
		}
		size += len(k) + 1 + len(v)
	}
	if size > 64<<10 {
		return invalidInput("parameters exceed 64 KiB")
	}
	return nil
}
func created(x *argoapi.WorkflowSummary, message string) *genargo.CreatedWorkflowResult {
	return &genargo.CreatedWorkflowResult{Status: "ok", Message: message, Workflow: workflowSummary(x)}
}
func workflowSummary(x *argoapi.WorkflowSummary) *genargo.WorkflowSummary {
	r := &genargo.WorkflowSummary{Name: x.Name, Namespace: x.Namespace, Status: x.Status}
	setPtr(&r.Progress, x.Progress)
	setPtr(&r.StartedAt, x.StartedAt)
	setPtr(&r.FinishedAt, x.FinishedAt)
	return r
}
func nilOrDenied(tool, ns, name string) *genargo.CreatedWorkflowResult {
	return &genargo.CreatedWorkflowResult{Status: "denied", Message: fmt.Sprintf("%s is disabled by server policy.", tool), Workflow: &genargo.WorkflowSummary{Name: name, Namespace: ns, Status: "not_created"}}
}
func invalidInput(message string) error { return genargo.MakeInvalidInput(fmt.Errorf("%s", message)) }
