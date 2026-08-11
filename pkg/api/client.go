package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
)

const defaultMaxBodyBytes = int64(16 << 20)

// Config configures the low-level protocol client.
type Config struct {
	HTTPClient    *http.Client
	Token         string
	DefaultBranch string
	UserAgent     string
	Headers       http.Header
	MaxBodyBytes  int64
}

// Client is a concurrency-safe Infrahub protocol client.
type Client struct {
	baseURL       *url.URL
	httpClient    *http.Client
	token         string
	defaultBranch string
	userAgent     string
	headers       http.Header
	maxBodyBytes  int64
}

// NewClient creates a low-level protocol client.
func NewClient(address string, config Config) (*Client, error) {
	baseURL, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("infrahub: parse address: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("infrahub: address scheme must be http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("infrahub: address must include a host")
	}
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("infrahub: HTTP client must not be nil")
	}
	if config.DefaultBranch == "" {
		return nil, fmt.Errorf("infrahub: default branch must not be empty")
	}
	if config.UserAgent == "" {
		return nil, fmt.Errorf("infrahub: user agent must not be empty")
	}
	if config.MaxBodyBytes == 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}
	if config.MaxBodyBytes < 0 {
		return nil, fmt.Errorf("infrahub: maximum response size must be positive")
	}
	baseURL.RawQuery, baseURL.Fragment = "", ""
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	baseURL.RawPath = strings.TrimRight(baseURL.RawPath, "/")
	return &Client{
		baseURL: baseURL, httpClient: config.HTTPClient, token: config.Token,
		defaultBranch: config.DefaultBranch, userAgent: config.UserAgent,
		headers: config.Headers.Clone(), maxBodyBytes: config.MaxBodyBytes,
	}, nil
}

// DefaultBranch returns the configured default branch.
func (c *Client) DefaultBranch() string { return c.defaultBranch }

// GraphQLRequest describes an arbitrary GraphQL operation.
type GraphQLRequest struct {
	Query         string
	Variables     map[string]any
	OperationName string
	Branch        string
	At            time.Time
	Tracker       string
	Headers       http.Header
}

// HTTPResponse contains the bounded body and metadata of a successful request.
type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type graphQLPayload struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage    `json:"data"`
	Errors []GraphQLErrorItem `json:"errors"`
}

// Execute runs a GraphQL operation and decodes its data into dst.
func (c *Client) Execute(ctx context.Context, request GraphQLRequest, dst any) error {
	if strings.TrimSpace(request.Query) == "" {
		return fmt.Errorf("infrahub: GraphQL query must not be empty")
	}
	payload, err := json.Marshal(graphQLPayload{request.Query, request.Variables, request.OperationName})
	if err != nil {
		return fmt.Errorf("infrahub: encode GraphQL request: %w", err)
	}
	tracker := request.Tracker
	if override, ok := requestcontext.Tracker(ctx); ok {
		tracker = override
	}
	body, err := c.Do(ctx, http.MethodPost, c.graphQLEndpoint(request.Branch, request.At), bytes.NewReader(payload), request.Headers, tracker)
	if err != nil {
		return err
	}
	var response graphQLResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("infrahub: decode GraphQL response: %w", err)
	}
	if dst != nil && len(response.Data) > 0 && string(response.Data) != "null" {
		if err := json.Unmarshal(response.Data, dst); err != nil {
			return fmt.Errorf("infrahub: decode GraphQL data: %w", err)
		}
	}
	if len(response.Errors) > 0 {
		return &GraphQLError{Items: response.Errors}
	}
	if len(response.Data) == 0 {
		return fmt.Errorf("infrahub: GraphQL response is missing data")
	}
	return nil
}

// Endpoint resolves an Infrahub REST path relative to the configured base URL.
func (c *Client) Endpoint(path string, query url.Values) *url.URL {
	u := *c.baseURL
	u.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return &u
}

// EndpointSegments resolves individually escaped REST path segments relative
// to the configured base URL.
func (c *Client) EndpointSegments(segments []string, query url.Values) *url.URL {
	u := *c.baseURL
	path := strings.TrimRight(c.baseURL.Path, "/")
	rawPath := strings.TrimRight(c.baseURL.EscapedPath(), "/")
	for _, segment := range segments {
		path += "/" + segment
		rawPath += "/" + escapePathSegment(segment)
	}
	u.Path = path
	u.RawPath = rawPath
	u.RawQuery = query.Encode()
	return &u
}

func escapePathSegment(segment string) string {
	switch segment {
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	default:
		return url.PathEscape(segment)
	}
}

// Do executes one HTTP request. It is exported for domain service packages;
// application code should normally use a higher-level service.
func (c *Client) Do(ctx context.Context, method string, endpoint *url.URL, body io.Reader, headers http.Header, tracker string) ([]byte, error) {
	response, err := c.DoResponse(ctx, method, endpoint, body, headers, tracker)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// DoResponse executes one HTTP request and returns its bounded body and
// response metadata. It is intended for domain services that need headers.
func (c *Client) DoResponse(ctx context.Context, method string, endpoint *url.URL, body io.Reader, headers http.Header, tracker string) (*HTTPResponse, error) {
	if override, ok := requestcontext.Tracker(ctx); ok {
		tracker = override
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("infrahub: build %s request: %w", method, err)
	}
	copyHeaders(req.Header, c.headers)
	copyHeaders(req.Header, headers)
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.token != "" {
		req.Header.Set("X-INFRAHUB-KEY", c.token)
	}
	if tracker != "" {
		req.Header.Set("X-Infrahub-Tracker", tracker)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("infrahub: %s %s: %w", method, endpoint.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("infrahub: read %s response: %w", method, err)
	}
	if int64(len(data)) > c.maxBodyBytes {
		return nil, fmt.Errorf("infrahub: response exceeds %d bytes", c.maxBodyBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{resp.StatusCode, method, endpoint.Redacted(), safeExcerpt(data, 1024, c.token)}
	}
	return &HTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: data}, nil
}

func (c *Client) graphQLEndpoint(branch string, at time.Time) *url.URL {
	if branch == "" {
		branch = c.defaultBranch
	}
	u := *c.baseURL
	basePath := strings.TrimRight(c.baseURL.Path, "/")
	baseEscaped := strings.TrimRight(c.baseURL.EscapedPath(), "/")
	u.Path = basePath + "/graphql/" + branch
	u.RawPath = baseEscaped + "/graphql/" + url.PathEscape(branch)
	if !at.IsZero() {
		query := u.Query()
		query.Set("at", at.Format(time.RFC3339Nano))
		u.RawQuery = query.Encode()
	}
	return &u
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func safeExcerpt(body []byte, limit int, secrets ...string) string {
	if len(body) > limit {
		body = body[:limit]
	}
	excerpt := string(body)
	for _, secret := range secrets {
		if secret != "" {
			excerpt = strings.ReplaceAll(excerpt, secret, "[REDACTED]")
		}
	}
	return excerpt
}
