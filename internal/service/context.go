package service

import (
	"context"
	"time"
	"unicode/utf8"

	genargo "github.com/CaliLuke/go-argo-mcp/gen/argo"
	"github.com/CaliLuke/go-argo-mcp/internal/version"
)

func (s *ArgoService) GetServerContext(ctx context.Context) (*genargo.ServerContextResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &genargo.ServerContextResult{
		BuildVersion: s.buildVersion, McpVersion: version.MCPVersion, Transport: s.transport,
		DefaultNamespace:  s.defaultNamespace,
		AllowedNamespaces: append([]string{}, s.policy.AllowedNamespaces...), DeniedNamespaces: append([]string{}, s.policy.DeniedNamespaces...),
		AllowMutations: s.policy.AllowMutations, AllowDestructive: s.policy.AllowDestructive, RequireConfirmation: s.policy.RequireConfirmation,
		ArgoConfigured: s.enabled(), KubernetesConfigured: s.kubernetes != nil,
		ArgoStatus: "unconfigured", Note: "Namespace policy is local server configuration and is not a Kubernetes RBAC enumeration.",
	}
	if !result.ArgoConfigured {
		return result, nil
	}
	result.ArgoStatus = "unavailable"
	versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	upstream, err := s.client.GetVersion(versionCtx)
	if parentErr := ctx.Err(); parentErr != nil {
		return nil, parentErr
	}
	if err == nil && upstream != "" && utf8.ValidString(upstream) && len(upstream) <= 256 {
		result.ArgoStatus = "available"
		result.ArgoVersion = &upstream
	}
	return result, nil
}
