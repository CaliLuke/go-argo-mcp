package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
)

func (s *ArgoService) GetResourceSpec(ctx context.Context, p *genargo.GetResourceSpecPayload) (*genargo.ResourceSpecResult, error) {
	kind, name := strings.TrimSpace(p.Kind), strings.TrimSpace(p.Name)
	if name == "" {
		return nil, invalidInput("name is required")
	}
	if !safeReadSegment(name) {
		return nil, invalidInput("name must be a safe path segment")
	}
	if kind != "workflow" && kind != "workflow_template" && kind != "cluster_workflow_template" && kind != "cron_workflow" {
		return nil, invalidInput("kind is invalid")
	}
	namespace := ""
	if kind == "cluster_workflow_template" {
		if p.Namespace != nil {
			return nil, invalidInput("namespace is forbidden for cluster-scoped templates")
		}
	} else {
		namespace = s.namespace(p.Namespace)
		if err := s.authorizeNamespace(namespace); err != nil {
			return nil, err
		}
		if !safeReadSegment(namespace) {
			return nil, invalidInput("namespace must be a safe path segment")
		}
	}
	section := p.Section
	if section == "" {
		section = "summary"
	}
	if section != "summary" && section != "arguments" && section != "templates" && section != "spec" {
		return nil, invalidInput("section is invalid")
	}
	templateName := pointerValue(p.TemplateName)
	if templateName != "" && section != "templates" {
		return nil, invalidInput("template_name is valid only with the templates section")
	}
	maxBytes := p.MaxBytes
	if maxBytes == 0 {
		maxBytes = 65536
	}
	if maxBytes < 1024 || maxBytes > 262144 {
		return nil, invalidInput("max_bytes must be between 1024 and 262144")
	}
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	resource, err := client.GetResourceSpec(ctx, kind, namespace, name)
	if err != nil {
		return nil, mapNewArgoError(err, argoTarget{action: "get spec for", resource: kind, namespace: namespace, name: name})
	}
	selected, err := selectResourceSpec(resource.Spec, kind, section, templateName)
	if err != nil {
		return nil, err
	}
	if len(selected) > maxBytes {
		return nil, invalidInput("selected spec exceeds max_bytes; request a narrower section or increase max_bytes")
	}
	result := &genargo.ResourceSpecResult{Kind: kind, Name: name, Section: section, SpecJSON: string(selected), Source: "argo"}
	if namespace != "" {
		result.Namespace = &namespace
	}
	if templateName != "" {
		result.TemplateName = &templateName
	}
	return result, nil
}

func selectResourceSpec(spec json.RawMessage, kind, section, templateName string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(spec, &fields); err != nil || fields == nil {
		return nil, invalidInput("Argo returned a malformed resource spec")
	}
	if kind == "cron_workflow" && section != "spec" {
		workflowSpec := fields["workflowSpec"]
		if len(workflowSpec) == 0 || string(workflowSpec) == "null" {
			workflowSpec = json.RawMessage(`{}`)
		}
		if err := json.Unmarshal(workflowSpec, &fields); err != nil || fields == nil {
			return nil, invalidInput("Argo returned a malformed cron workflowSpec")
		}
		spec = workflowSpec
	}
	switch section {
	case "spec":
		return compactJSON(spec)
	case "arguments":
		return selectedField(fields, "arguments", `{}`)
	case "templates":
		templates, err := selectedField(fields, "templates", `[]`)
		if err != nil || templateName == "" {
			return templates, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(templates, &items); err != nil {
			return nil, invalidInput("Argo returned malformed templates")
		}
		var match json.RawMessage
		matches := 0
		for _, item := range items {
			var identity struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(item, &identity) == nil && identity.Name == templateName {
				match = item
				matches++
			}
		}
		if matches == 1 {
			return compactJSON(match)
		}
		if matches > 1 {
			return nil, invalidInput("template_name matched more than one template")
		}
		return nil, genargo.MakeArgoNotFound(fmt.Errorf("template %q was not found", templateName))
	case "summary":
		var templates []struct {
			Name string `json:"name"`
		}
		if raw := fields["templates"]; len(raw) != 0 && json.Unmarshal(raw, &templates) != nil {
			return nil, invalidInput("Argo returned malformed templates")
		}
		names := make([]string, 0, len(templates))
		for _, template := range templates {
			names = append(names, template.Name)
		}
		summary := map[string]any{"entrypoint": "", "arguments": map[string]any{}, "template_names": names, "workflowTemplateRef": map[string]any{}}
		if raw := fields["entrypoint"]; len(raw) != 0 {
			var entrypoint string
			if err := json.Unmarshal(raw, &entrypoint); err != nil {
				return nil, invalidInput("Argo returned malformed entrypoint")
			}
			summary["entrypoint"] = entrypoint
		}
		for _, key := range []string{"arguments", "workflowTemplateRef"} {
			if raw := fields[key]; len(raw) != 0 {
				var value any
				decoder := json.NewDecoder(strings.NewReader(string(raw)))
				decoder.UseNumber()
				if err := decoder.Decode(&value); err != nil {
					return nil, invalidInput("Argo returned malformed " + key)
				}
				summary[key] = value
			}
		}
		return json.Marshal(summary)
	default:
		return nil, invalidInput("section is invalid")
	}
}

func selectedField(fields map[string]json.RawMessage, key, empty string) ([]byte, error) {
	raw := fields[key]
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(empty)
	}
	return compactJSON(raw)
}

func compactJSON(raw json.RawMessage) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, invalidInput("Argo returned malformed spec JSON")
	}
	return json.Marshal(value)
}
