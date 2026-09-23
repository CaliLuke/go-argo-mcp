package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var bearerTokenPattern = regexp.MustCompile(`^[A-Za-z0-9\-._~+/]+={0,}$`)

type httpSecurityPolicy struct {
	token          [32]byte
	auth           bool
	allowedHosts   map[string]struct{}
	allowedOrigins map[string]struct{}
	origins        []string
}

func validateBearerToken(value string) error {
	if value == "" || strings.TrimSpace(value) != value || !bearerTokenPattern.MatchString(value) {
		return fmt.Errorf("invalid bearer token")
	}
	return nil
}

func buildHTTPSecurityPolicy(cfg Config) (*httpSecurityPolicy, error) {
	host, port, err := splitAuthority(cfg.Addr, true, "")
	if err != nil {
		return nil, fmt.Errorf("invalid ARGO_MCP_ADDR")
	}
	loopback := host == "localhost"
	if ip, err := netip.ParseAddr(host); err == nil {
		loopback = ip.Unmap().IsLoopback()
	}
	if !loopback && cfg.AuthToken == "" && !cfg.AllowUnauthenticated {
		return nil, fmt.Errorf("non-loopback HTTP requires ARGO_MCP_AUTH_TOKEN or ARGO_MCP_ALLOW_UNAUTHENTICATED=true")
	}
	p := &httpSecurityPolicy{allowedHosts: map[string]struct{}{}, allowedOrigins: map[string]struct{}{}}
	if cfg.AuthToken != "" {
		if err := validateBearerToken(cfg.AuthToken); err != nil {
			return nil, fmt.Errorf("invalid ARGO_MCP_AUTH_TOKEN")
		}
		p.auth = true
		p.token = sha256.Sum256([]byte(cfg.AuthToken))
	}
	if len(cfg.AllowedHosts) == 0 {
		if host == "" || host == "0.0.0.0" || host == "::" {
			return nil, fmt.Errorf("wildcard bind requires ARGO_MCP_ALLOWED_HOSTS")
		}
		if loopback {
			for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
				p.allowedHosts[net.JoinHostPort(h, port)] = struct{}{}
			}
		} else {
			p.allowedHosts[net.JoinHostPort(host, port)] = struct{}{}
		}
	} else {
		for _, raw := range cfg.AllowedHosts {
			h, po, parseErr := splitAuthority(raw, true, "")
			if parseErr != nil {
				return nil, fmt.Errorf("invalid ARGO_MCP_ALLOWED_HOSTS")
			}
			key := net.JoinHostPort(h, po)
			if _, duplicate := p.allowedHosts[key]; duplicate {
				return nil, fmt.Errorf("duplicate ARGO_MCP_ALLOWED_HOSTS entry")
			}
			p.allowedHosts[key] = struct{}{}
		}
	}
	for _, raw := range cfg.AllowedOrigins {
		origin, parseErr := canonicalOrigin(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid ARGO_MCP_ALLOWED_ORIGINS")
		}
		if _, duplicate := p.allowedOrigins[origin]; duplicate {
			return nil, fmt.Errorf("duplicate ARGO_MCP_ALLOWED_ORIGINS entry")
		}
		p.allowedOrigins[origin] = struct{}{}
		p.origins = append(p.origins, origin)
	}
	return p, nil
}

func splitAuthority(raw string, requirePort bool, defaultPort string) (string, string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "@/%?#\\") {
		return "", "", fmt.Errorf("invalid authority")
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil && !requirePort {
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			host, port, err = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"), defaultPort, nil
		} else {
			host, port, err = raw, defaultPort, nil
			if strings.Contains(raw, ":") {
				return "", "", fmt.Errorf("IPv6 must be bracketed")
			}
		}
	}
	if err != nil || (requirePort && port == "") {
		return "", "", fmt.Errorf("invalid authority")
	}
	if host == "" && raw != ":"+port {
		return "", "", fmt.Errorf("invalid authority")
	}
	if host != "" {
		var normalizeErr error
		host, normalizeErr = canonicalHost(host)
		if normalizeErr != nil {
			return "", "", normalizeErr
		}
	}
	if port == "" {
		return "", "", fmt.Errorf("invalid port")
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return "", "", fmt.Errorf("invalid port")
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("invalid port")
	}
	return host, strconv.Itoa(n), nil
}

func canonicalHost(host string) (string, error) {
	if strings.HasSuffix(host, ".") || strings.Contains(host, "%") {
		return "", fmt.Errorf("invalid host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap().String(), nil
	}
	host = strings.ToLower(host)
	if host == "" || strings.ContainsAny(host, "[]: ") {
		return "", fmt.Errorf("invalid host")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("invalid host")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", fmt.Errorf("invalid host")
			}
		}
	}
	return host, nil
}

func canonicalOrigin(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.HasSuffix(raw, "/") || strings.Contains(raw, "#") {
		return "", fmt.Errorf("invalid origin")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return "", fmt.Errorf("invalid origin")
	}
	host, err := canonicalHost(u.Hostname())
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid port")
	}
	authority := net.JoinHostPort(host, strconv.Itoa(n))
	if (u.Scheme == "http" && n == 80) || (u.Scheme == "https" && n == 443) {
		authority = host
		if strings.Contains(host, ":") {
			authority = "[" + host + "]"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + authority, nil
}

func (p *httpSecurityPolicy) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultPort := "80"
		if r.TLS != nil {
			defaultPort = "443"
		}
		host, port, err := splitAuthority(r.Host, false, defaultPort)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if _, ok := p.allowedHosts[net.JoinHostPort(host, port)]; !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		origins := r.Header.Values("Origin")
		if len(origins) > 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if len(origins) == 1 {
			origin, originErr := canonicalOrigin(origins[0])
			directScheme := "http"
			if r.TLS != nil {
				directScheme = "https"
			}
			direct, _ := canonicalOrigin(directScheme + "://" + net.JoinHostPort(host, port))
			_, trusted := p.allowedOrigins[origin]
			if originErr != nil || (origin != direct && !trusted) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			r = r.Clone(r.Context())
			r.Header.Set("Origin", origin)
		}
		if p.auth {
			values := r.Header.Values("Authorization")
			if len(values) != 1 {
				unauthorized(w)
				return
			}
			scheme, token, ok := parseBearerAuthorization(values[0])
			if !ok || !strings.EqualFold(scheme, "Bearer") {
				unauthorized(w)
				return
			}
			digest := sha256.Sum256([]byte(token))
			if subtle.ConstantTimeCompare(digest[:], p.token[:]) != 1 {
				unauthorized(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func parseBearerAuthorization(value string) (string, string, bool) {
	value = strings.Trim(value, " \t")
	separator := strings.IndexByte(value, ' ')
	if separator <= 0 {
		return "", "", false
	}
	scheme := value[:separator]
	token := strings.TrimLeft(value[separator:], " ")
	if token == "" || strings.ContainsAny(token, " \t\r\n") || validateBearerToken(token) != nil {
		return "", "", false
	}
	return scheme, token, true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="argo-mcp"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
