// Package objectstore provides access to Infrahub object and file storage.
package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const (
	trackerGetObject = "object-store-get"
	trackerUpload    = "object-store-upload"
	trackerGetFile   = "object-store-file-get"
)

var allowedTextMediaTypes = map[string]struct{}{
	"application/json":   {},
	"application/yaml":   {},
	"application/x-yaml": {},
}

// Client is the minimal REST behavior required by Service.
type Client interface {
	// EndpointSegments resolves individually escaped REST path segments.
	EndpointSegments([]string, url.Values) *url.URL
	// DoResponse executes a bounded HTTP request and preserves response metadata.
	DoResponse(context.Context, string, *url.URL, io.Reader, http.Header, string) (*api.HTTPResponse, error)
}

// Service manages Infrahub object and file storage.
type Service struct{ client Client }

// NewService creates an object-store service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// UploadResult identifies content stored by Infrahub.
type UploadResult struct {
	// Identifier is the object or resource identifier.
	Identifier string `json:"identifier"`
	// Checksum contains the checksum value.
	Checksum string `json:"checksum"`
}

// UnsupportedContentTypeError reports binary content returned by a text-only
// file method.
type UnsupportedContentTypeError struct {
	// Identifier is the object or resource identifier.
	Identifier string
	// ContentType contains the content type value.
	ContentType string
}

// Error reports the unsupported response content type.
func (e *UnsupportedContentTypeError) Error() string {
	return fmt.Sprintf("infrahub: binary content type %q is not supported for file %q", e.ContentType, e.Identifier)
}

// Get returns object-store content by identifier without MIME restrictions.
func (s *Service) Get(ctx context.Context, identifier string) (string, error) {
	if identifier == "" {
		return "", fmt.Errorf("infrahub: object-store identifier must not be empty")
	}
	response, err := s.get(ctx, []string{"api", "storage", "object", identifier}, nil, trackerGetObject)
	if err != nil {
		return "", err
	}
	return string(response.Body), nil
}

// Upload stores textual content and returns its identifier and checksum.
func (s *Service) Upload(ctx context.Context, content string) (*UploadResult, error) {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return nil, fmt.Errorf("infrahub: encode object-store upload: %w", err)
	}
	response, err := s.client.DoResponse(
		ctx, http.MethodPost,
		s.client.EndpointSegments([]string{"api", "storage", "upload", "content"}, nil),
		bytes.NewReader(body), http.Header{"Content-Type": {"application/json"}}, trackerUpload,
	)
	if err != nil {
		return nil, err
	}
	var result UploadResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return nil, fmt.Errorf("infrahub: decode object-store upload: %w", err)
	}
	if result.Identifier == "" || result.Checksum == "" {
		return nil, fmt.Errorf("infrahub: object-store upload response is missing identifier or checksum")
	}
	return &result, nil
}

// GetFileByStorageID returns text file content by storage identifier.
func (s *Service) GetFileByStorageID(ctx context.Context, storageID string) (string, error) {
	if storageID == "" {
		return "", fmt.Errorf("infrahub: storage ID must not be empty")
	}
	return s.getTextFile(ctx, []string{"api", "storage", "files", "by-storage-id", storageID}, nil, storageID)
}

// GetFileByID returns text file content by Infrahub node ID.
func (s *Service) GetFileByID(ctx context.Context, nodeID string) (string, error) {
	if nodeID == "" {
		return "", fmt.Errorf("infrahub: file node ID must not be empty")
	}
	return s.getTextFile(ctx, []string{"api", "storage", "files", nodeID}, nil, nodeID)
}

// GetFileByHFID returns text file content by kind and human-friendly ID.
func (s *Service) GetFileByHFID(ctx context.Context, kind string, hfid []string) (string, error) {
	if kind == "" {
		return "", fmt.Errorf("infrahub: file kind must not be empty")
	}
	if len(hfid) == 0 {
		return "", fmt.Errorf("infrahub: file HFID must not be empty")
	}
	query := url.Values{"hfid": append([]string(nil), hfid...)}
	return s.getTextFile(ctx, []string{"api", "storage", "files", "by-hfid", kind}, query, kind+":"+strings.Join(hfid, "/"))
}

// getTextFile gets the text file.
func (s *Service) getTextFile(ctx context.Context, segments []string, query url.Values, identifier string) (string, error) {
	response, err := s.get(ctx, segments, query, trackerGetFile)
	if err != nil {
		return "", err
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return "", &UnsupportedContentTypeError{Identifier: identifier, ContentType: response.Header.Get("Content-Type")}
	}
	mediaType = strings.ToLower(mediaType)
	if !strings.HasPrefix(mediaType, "text/") {
		if _, allowed := allowedTextMediaTypes[mediaType]; !allowed {
			return "", &UnsupportedContentTypeError{Identifier: identifier, ContentType: mediaType}
		}
	}
	return string(response.Body), nil
}

// get retrieves stored text and rejects unsupported response content types.
func (s *Service) get(ctx context.Context, segments []string, query url.Values, tracker string) (*api.HTTPResponse, error) {
	return s.client.DoResponse(ctx, http.MethodGet, s.client.EndpointSegments(segments, query), nil, nil, tracker)
}
