package server

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestConfigTransportDefaultsAndValidation(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      Transport
		wantError bool
	}{
		{name: "unset", want: TransportHTTP},
		{name: "empty", value: "", want: TransportHTTP},
		{name: "http", value: "http", want: TransportHTTP},
		{name: "stateless", value: "http-stateless", want: TransportHTTPStateless},
		{name: "stdio", value: "stdio", want: TransportStdio},
		{name: "invalid", value: "sse", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ConfigFromLookup(func(key string) (string, bool) {
				if key == "ARGO_MCP_TRANSPORT" {
					return tt.value, tt.name != "unset"
				}
				return "", false
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("ConfigFromLookup error = %v, wantError %t", err, tt.wantError)
			}
			if cfg.Transport != tt.want {
				t.Fatalf("transport = %q, want %q", cfg.Transport, tt.want)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := ConfigFromLookup(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8080" || cfg.DefaultNamespace != "default" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if !cfg.AuditEnabled || cfg.AuditFile != "./mcp-audit.log" {
		t.Fatalf("unexpected audit defaults: %#v", cfg)
	}
}

func TestStatelessRejectsSessionCompatibilityFlagConfig(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "enabled", value: "other=1,allowsessionsinstateless=1", wantError: true},
		{name: "whitespace around key", value: "allowsessionsinstateless =1", wantError: true},
		{name: "whitespace around value", value: "allowsessionsinstateless= 1", wantError: true},
		{name: "last duplicate enables", value: "allowsessionsinstateless=0,allowsessionsinstateless=1", wantError: true},
		{name: "last duplicate disables", value: "allowsessionsinstateless=1,allowsessionsinstateless=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ConfigFromLookup(func(key string) (string, bool) {
				switch key {
				case "ARGO_MCP_TRANSPORT":
					return "http-stateless", true
				case "MCPGODEBUG":
					return tt.value, true
				default:
					return "", false
				}
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("ConfigFromLookup error = %v, wantError %t", err, tt.wantError)
			}
		})
	}
}

func TestStdioRejectsStdoutAudit(t *testing.T) {
	procPath := filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd", "1")
	paths := []string{"/dev/stdout", "/dev/fd/1", "/proc/self/fd/1", procPath}
	dir := t.TempDir()
	symlink := filepath.Join(dir, "audit.log")
	if err := os.Symlink("/dev/stdout", symlink); err == nil {
		paths = append(paths, symlink)
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := ConfigFromLookup(func(key string) (string, bool) {
				switch key {
				case "ARGO_MCP_TRANSPORT":
					return "stdio", true
				case "MCP_AUDIT_FILE":
					return path, true
				default:
					return "", false
				}
			}); err == nil && path != procPath && path != "/proc/self/fd/1" {
				t.Fatalf("expected stdout audit rejection for %q", path)
			}
		})
	}
}
