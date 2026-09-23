package argoapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

type ArtifactContent struct {
	Text          string
	OffsetBytes   int
	ReturnedBytes int
	NextOffset    int
	HasMore       bool
}

func (c *Client) ReadArtifactContent(ctx context.Context, namespace, workflow, archiveUID, nodeID, direction, artifact string, offset, maxBytes int) (*ArtifactContent, error) {
	var endpoint string
	var err error
	if archiveUID == "" {
		endpoint, err = c.ArtifactURL(namespace, workflow, nodeID, direction, artifact)
	} else {
		endpoint, err = c.ArchivedArtifactURL(namespace, archiveUID, nodeID, direction, artifact)
	}
	if err != nil {
		return nil, err
	}
	return c.readArtifactURL(ctx, endpoint, offset, maxBytes)
}

func (c *Client) readArtifactURL(ctx context.Context, endpoint string, offset, maxBytes int) (*ArtifactContent, error) {
	if offset < 0 || maxBytes <= 0 {
		return nil, fmt.Errorf("invalid artifact byte bounds")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Argo base URL: %w", err)
	}
	current := endpoint
	for redirects := 0; ; redirects++ {
		req, err := c.newRequest(ctx, http.MethodGet, current, nil, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/plain")
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GET artifact: %w", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			_ = resp.Body.Close()
			if redirects >= 5 {
				return nil, fmt.Errorf("artifact redirect limit exceeded")
			}
			location, err := resp.Location()
			if err != nil {
				return nil, fmt.Errorf("invalid artifact redirect: %w", err)
			}
			if !sameOrigin(base, location) {
				return nil, fmt.Errorf("artifact redirect changed origin")
			}
			current = location.String()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, &HTTPError{StatusCode: resp.StatusCode, Endpoint: endpoint}
		}
		encoding := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
		if encoding != "" && !strings.EqualFold(encoding, "identity") {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("artifact response uses unsupported content encoding")
		}
		readLimit := int64(offset) + int64(maxBytes) + utf8.UTFMax
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, readLimit))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read artifact content: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close artifact content: %w", closeErr)
		}
		bounded := int64(len(data)) == readLimit && (resp.ContentLength < 0 || resp.ContentLength > readLimit)
		return artifactPage(data, offset, maxBytes, bounded)
	}
}

func artifactPage(data []byte, offset, maxBytes int, bounded bool) (*ArtifactContent, error) {
	validEnd, err := validArtifactUTF8Prefix(data, bounded)
	if err != nil || strings.IndexByte(string(data[:validEnd]), 0) >= 0 {
		return nil, fmt.Errorf("artifact content is not valid UTF-8 text")
	}
	if offset > validEnd {
		return nil, fmt.Errorf("artifact offset is beyond retained content")
	}
	if offset < validEnd && !utf8.RuneStart(data[offset]) {
		return nil, fmt.Errorf("artifact offset splits a UTF-8 character")
	}
	end := offset + maxBytes
	if end > validEnd {
		end = validEnd
	}
	for end > offset && !utf8.Valid(data[offset:end]) {
		end--
	}
	if end == offset && offset < validEnd {
		_, width := utf8.DecodeRune(data[offset:])
		if width > maxBytes {
			return nil, fmt.Errorf("max_bytes is too small for the next UTF-8 character")
		}
	}
	returned := end - offset
	more := bounded || end < validEnd
	result := &ArtifactContent{Text: string(data[offset:end]), OffsetBytes: offset, ReturnedBytes: returned, HasMore: more}
	if more {
		result.NextOffset = end
	}
	return result, nil
}

func validArtifactUTF8Prefix(data []byte, bounded bool) (int, error) {
	for offset := 0; offset < len(data); {
		if !utf8.FullRune(data[offset:]) {
			if bounded {
				return offset, nil
			}
			return 0, fmt.Errorf("incomplete UTF-8 character")
		}
		r, size := utf8.DecodeRune(data[offset:])
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("malformed UTF-8 byte")
		}
		offset += size
	}
	return len(data), nil
}

func sameOrigin(base, target *url.URL) bool {
	return target.User == nil && strings.EqualFold(base.Scheme, target.Scheme) && strings.EqualFold(base.Host, target.Host)
}
