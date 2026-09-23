package mcpvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	loom "github.com/CaliLuke/loom/pkg"

	mcpargo "github.com/CaliLuke/go-argo-mcp/gen/mcp_argo"
)

var paginatedTools = map[string]struct{}{
	"list_workflows":                  {},
	"list_cron_workflows":             {},
	"get_cron_history":                {},
	"list_workflow_templates":         {},
	"list_cluster_workflow_templates": {},
}

func PaginationLimits() mcpargo.ToolCallInterceptor {
	return func(ctx context.Context, info mcpargo.ToolCallInterceptorInfo, payload *mcpargo.ToolsCallPayload, next mcpargo.ToolCallHandler) (*mcpargo.ToolsCallResult, error) {
		if _, paginated := paginatedTools[info.Tool()]; paginated {
			if err := validateExplicitLimit(info.RawArguments()); err != nil {
				return nil, loom.PermanentError("invalid_params", "%s", err)
			}
		}
		return next(ctx, payload)
	}
}

func validateExplicitLimit(arguments []byte) error {
	trimmed := bytes.TrimSpace(arguments)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("decode pagination arguments: %w", err)
	}
	raw, present := fields["limit"]
	if !present {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("limit must be an integer between 1 and 200")
	}
	var limit int
	if err := json.Unmarshal(raw, &limit); err != nil || limit < 1 || limit > 200 {
		return fmt.Errorf("limit must be an integer between 1 and 200")
	}
	return nil
}
