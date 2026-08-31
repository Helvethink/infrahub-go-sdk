// Package schema provides operations for Infrahub's branch-aware schemas.
package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client is the minimal protocol required by Service.
type Client interface {
	// DefaultBranch is used when a request does not select a branch.
	DefaultBranch() string
	// Endpoint resolves an Infrahub REST endpoint against the configured base URL.
	Endpoint(string, url.Values) *url.URL
	// Do executes a bounded HTTP request.
	Do(context.Context, string, *url.URL, io.Reader, http.Header, string) ([]byte, error)
}

// Service accesses Infrahub's schema APIs.
type Service struct{ client Client }

// NewService creates a schema service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// Fetch retrieves the raw Infrahub schema for a branch.
func (s *Service) Fetch(ctx context.Context, branch string, namespaces []string, dst any) error {
	if branch == "" {
		branch = s.client.DefaultBranch()
	}
	query := url.Values{"branch": {branch}}
	for _, namespace := range namespaces {
		query.Add("namespaces", namespace)
	}
	body, err := s.client.Do(ctx, http.MethodGet, s.client.Endpoint("api/schema", query), nil, nil, "")
	if err != nil {
		return err
	}
	if err := decode(body, dst); err != nil {
		return fmt.Errorf("infrahub: decode schema: %w", err)
	}
	return nil
}

// GraphQL retrieves the GraphQL SDL for a branch.
func (s *Service) GraphQL(ctx context.Context, branch string) (string, error) {
	if branch == "" {
		branch = s.client.DefaultBranch()
	}
	body, err := s.client.Do(ctx, http.MethodGet, s.client.Endpoint("schema.graphql", url.Values{"branch": {branch}}), nil, nil, "")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Load loads schema documents into a branch and returns the raw response.
func (s *Service) Load(ctx context.Context, branch string, schemas []map[string]any, dst any) error {
	return s.post(ctx, "api/schema/load", branch, schemas, dst)
}

// Check validates schema documents without loading them.
func (s *Service) Check(ctx context.Context, branch string, schemas []map[string]any, dst any) error {
	return s.post(ctx, "api/schema/check", branch, schemas, dst)
}

// post sends schema documents to a schema endpoint and decodes its response.
func (s *Service) post(ctx context.Context, path, branch string, schemas []map[string]any, dst any) error {
	if branch == "" {
		branch = s.client.DefaultBranch()
	}
	payload, err := json.Marshal(map[string]any{"schemas": schemas})
	if err != nil {
		return fmt.Errorf("infrahub: encode schemas: %w", err)
	}
	body, err := s.client.Do(ctx, http.MethodPost, s.client.Endpoint(path, url.Values{"branch": {branch}}), bytes.NewReader(payload), nil, "")
	if err != nil {
		return err
	}
	if err := decode(body, dst); err != nil {
		return fmt.Errorf("infrahub: decode schema response: %w", err)
	}
	return nil
}

// decode unmarshals a bounded response when a destination was supplied.
func decode(data []byte, dst any) error {
	if dst == nil {
		return nil
	}
	return json.Unmarshal(data, dst)
}
