package service

import "github.com/CaliLuke/go-argo-mcp/internal/argoapi"

func safeReadSegment(value string) bool {
	return argoapi.ValidatePathSegment(value) == nil
}
