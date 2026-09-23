package argoapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const maxResourceSpecBody = 8 << 20

type ResourceSpec struct {
	Name      string
	Namespace string
	Spec      json.RawMessage
}

func (c *Client) GetResourceSpec(ctx context.Context, kind, namespace, name string) (*ResourceSpec, error) {
	var endpoint string
	switch kind {
	case "workflow":
		endpoint = c.baseURL + "/api/v1/workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	case "workflow_template":
		endpoint = c.baseURL + "/api/v1/workflow-templates/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	case "cluster_workflow_template":
		endpoint = c.baseURL + "/api/v1/cluster-workflow-templates/" + url.PathEscape(name)
	case "cron_workflow":
		endpoint = c.baseURL + "/api/v1/cron-workflows/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
	default:
		return nil, fmt.Errorf("unsupported resource kind")
	}
	var response struct {
		Metadata struct {
			Name, Namespace string
		} `json:"metadata"`
		Spec json.RawMessage `json:"spec"`
	}
	if err := c.doBoundedJSON(ctx, endpoint, maxResourceSpecBody, &response); err != nil {
		return nil, err
	}
	if response.Metadata.Name != name || kind != "cluster_workflow_template" && response.Metadata.Namespace != namespace || kind == "cluster_workflow_template" && response.Metadata.Namespace != "" {
		return nil, fmt.Errorf("resource identity mismatch")
	}
	if len(response.Spec) == 0 || string(response.Spec) == "null" {
		response.Spec = json.RawMessage(`{}`)
	}
	return &ResourceSpec{Name: response.Metadata.Name, Namespace: response.Metadata.Namespace, Spec: response.Spec}, nil
}

func (c *Client) GetVersion(ctx context.Context) (string, error) {
	var response struct {
		Version string `json:"version"`
	}
	if err := c.doBoundedJSON(ctx, c.baseURL+"/api/v1/version", 1<<20, &response); err != nil {
		return "", err
	}
	return response.Version, nil
}

func (c *Client) doBoundedJSON(ctx context.Context, endpoint string, maxBytes int64, destination any) error {
	req, err := c.newRequest(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("read body from %s: %w", endpoint, err)
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("response from %s exceeds %d bytes", endpoint, maxBytes)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode response from %s: %w", endpoint, err)
	}
	return nil
}
