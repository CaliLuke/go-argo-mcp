package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Transport string

const (
	TransportHTTP          Transport = "http"
	TransportHTTPStateless Transport = "http-stateless"
	TransportStdio         Transport = "stdio"
)

type Config struct {
	Transport Transport
	Addr      string
	Version   string

	ArgoBaseURL            string
	ArgoToken              string
	ArgoUsername           string
	ArgoPassword           string
	ArgoInsecureSkipVerify bool
	ArgoTLSServerName      string
	ArgoRequestTimeout     time.Duration
	DefaultNamespace       string

	AllowMutations      bool
	AllowDestructive    bool
	RequireConfirmation bool
	AllowedNamespaces   []string
	DeniedNamespaces    []string

	AuditEnabled bool
	AuditFile    string

	ShutdownTimeout time.Duration
}

func ConfigFromEnv(version string) (Config, error) {
	cfg, err := ConfigFromLookup(os.Getenv)
	cfg.Version = version
	return cfg, err
}

func ConfigFromLookup(lookup func(string) string) (Config, error) {
	mode := Transport(strings.TrimSpace(lookup("ARGO_MCP_TRANSPORT")))
	if mode == "" {
		mode = TransportHTTP
	}
	if mode != TransportHTTP && mode != TransportHTTPStateless && mode != TransportStdio {
		return Config{}, fmt.Errorf("invalid ARGO_MCP_TRANSPORT %q: must be http, http-stateless, or stdio", mode)
	}
	cfg := Config{
		Transport:              mode,
		Addr:                   envOrDefault(lookup, "ARGO_MCP_ADDR", "127.0.0.1:8080"),
		ArgoBaseURL:            lookup("ARGO_BASE_URL"),
		ArgoToken:              lookup("ARGO_TOKEN"),
		ArgoUsername:           lookup("ARGO_USERNAME"),
		ArgoPassword:           lookup("ARGO_PASSWORD"),
		ArgoInsecureSkipVerify: envBool(lookup, "ARGO_INSECURE_SKIP_TLS_VERIFY", false),
		ArgoTLSServerName:      lookup("ARGO_TLS_SERVER_NAME"),
		ArgoRequestTimeout:     envDurationSeconds(lookup, "ARGO_REQUEST_TIMEOUT_SECONDS", 30),
		DefaultNamespace:       envOrDefault(lookup, "ARGO_NAMESPACE", "default"),
		AllowMutations:         envBool(lookup, "MCP_ALLOW_MUTATIONS", false),
		AllowDestructive:       envBool(lookup, "MCP_ALLOW_DESTRUCTIVE", false),
		RequireConfirmation:    envBool(lookup, "MCP_REQUIRE_CONFIRMATION", true),
		AllowedNamespaces:      envCSV(lookup, "MCP_NAMESPACES_ALLOW"),
		DeniedNamespaces:       envCSV(lookup, "MCP_NAMESPACES_DENY"),
		AuditEnabled:           envBool(lookup, "MCP_AUDIT_ENABLED", true),
		AuditFile:              envOrDefault(lookup, "MCP_AUDIT_FILE", "./mcp-audit.log"),
		ShutdownTimeout:        5 * time.Second,
	}
	if mode == TransportHTTPStateless && debugFlagEnabled(lookup("MCPGODEBUG"), "allowsessionsinstateless") {
		return Config{}, fmt.Errorf("MCPGODEBUG allowsessionsinstateless=1 is incompatible with ARGO_MCP_TRANSPORT=http-stateless")
	}
	if mode == TransportStdio && cfg.AuditEnabled && isStdoutPath(cfg.AuditFile) {
		return Config{}, fmt.Errorf("MCP_AUDIT_FILE %q resolves to stdout and would corrupt stdio MCP frames", cfg.AuditFile)
	}
	return cfg, nil
}

func envOrDefault(lookup func(string) string, key, fallback string) string {
	if value := lookup(key); value != "" {
		return value
	}
	return fallback
}

func envBool(lookup func(string) string, key string, fallback bool) bool {
	value := strings.TrimSpace(lookup(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(lookup func(string) string, key string, fallback int) time.Duration {
	seconds, err := strconv.Atoi(lookup(key))
	if err != nil || seconds <= 0 {
		seconds = fallback
	}
	return time.Duration(seconds) * time.Second
}

func envCSV(lookup func(string) string, key string) []string {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func debugFlagEnabled(raw, name string) bool {
	value := ""
	found := false
	for _, item := range strings.Split(raw, ",") {
		key, itemValue, ok := strings.Cut(item, "=")
		if ok && strings.TrimSpace(key) == name {
			value = strings.TrimSpace(itemValue)
			found = true
		}
	}
	return found && value == "1"
}

func isStdoutPath(path string) bool {
	cleaned := filepath.Clean(path)
	pidFD := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd", "1")
	if isNamedStdoutPath(cleaned, pidFD) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && isNamedStdoutPath(filepath.Clean(resolved), pidFD) {
		return true
	}
	stdoutInfo, stdoutErr := os.Stdout.Stat()
	pathInfo, pathErr := os.Stat(path)
	return stdoutErr == nil && pathErr == nil && os.SameFile(stdoutInfo, pathInfo)
}

func isNamedStdoutPath(path, pidFD string) bool {
	return path == "/dev/stdout" || path == "/dev/fd/1" || path == "/proc/self/fd/1" || path == pidFD
}
