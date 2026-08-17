package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (failingBody) Close() error               { return nil }

type receivedPayload struct {
	OperationName string `json:"operationName"`
}

func testConfig(hc *http.Client) Config {
	return Config{HTTPClient: hc, DefaultBranch: "main", UserAgent: "test", MaxBodyBytes: 16 << 20}
}

func TestExecuteRequestAndPartialData(t *testing.T) {
	t.Parallel()
	var gotPath, gotQuery, gotToken, gotTracker string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		gotToken, gotTracker = r.Header.Get("X-INFRAHUB-KEY"), r.Header.Get("X-Infrahub-Tracker")
		var payload receivedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.OperationName != "Example" {
			t.Errorf("operationName = %q", payload.OperationName)
		}
		_, _ = w.Write([]byte(`{"data":{"value":42},"errors":[{"message":"partial failure","path":["value"]}]}`))
	}))
	defer server.Close()
	cfg := testConfig(server.Client())
	cfg.Token = "top-secret"
	client, err := NewClient(server.URL+"/base", cfg)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Value int `json:"value"`
	}
	err = client.Execute(context.Background(), GraphQLRequest{
		Query: "query Example { value }", OperationName: "Example", Branch: "feature/a b",
		At: time.Date(2025, 1, 2, 3, 4, 5, 6, time.UTC), Tracker: "test",
	}, &data)
	var gqlErr *GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if data.Value != 42 || gotPath != "/base/graphql/feature%2Fa%20b" {
		t.Errorf("data=%#v path=%q", data, gotPath)
	}
	if !strings.Contains(gotQuery, "at=2025-01-02T03%3A04%3A05.000000006Z") {
		t.Errorf("query = %q", gotQuery)
	}
	if gotToken != "top-secret" || gotTracker != "test" {
		t.Errorf("headers token=%q tracker=%q", gotToken, gotTracker)
	}
}

func TestContextTrackerOverridesAndSuppressesRequestTracker(t *testing.T) {
	t.Parallel()
	trackers := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trackers <- r.Header.Get("X-Infrahub-Tracker")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, testConfig(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	request := GraphQLRequest{Query: "query Test { ok }", Tracker: "service-default"}
	if err := client.Execute(requestcontext.WithTracker(context.Background(), "workflow"), request, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Execute(requestcontext.WithTracker(context.Background(), ""), request, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-trackers; got != "workflow" {
		t.Fatalf("override tracker = %q", got)
	}
	if got := <-trackers; got != "" {
		t.Fatalf("suppressed tracker = %q", got)
	}
}

func TestHTTPErrorRedactsCredentials(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token never-print-me denied"}`))
	}))
	defer server.Close()
	cfg := testConfig(server.Client())
	cfg.Token = "never-print-me"
	client, err := NewClient(server.URL, cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if strings.Contains(httpErr.Body, "never-print-me") || !strings.Contains(httpErr.Body, "[REDACTED]") {
		t.Fatalf("body = %q", httpErr.Body)
	}
}

func TestExecuteResponseLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"long":"value"}}`))
	}))
	defer server.Close()
	cfg := testConfig(server.Client())
	cfg.MaxBodyBytes = 8
	client, err := NewClient(server.URL, cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, err := NewClient(server.URL, testConfig(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = client.Execute(ctx, GraphQLRequest{Query: "query { x }"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestConfiguredRedirectPolicyIsHonored(t *testing.T) {
	t.Parallel()
	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalled.Store(true) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	hc := server.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client, err := NewClient(server.URL, testConfig(hc))
	if err != nil {
		t.Fatal(err)
	}
	err = client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %T %v", err, err)
	}
	if targetCalled.Load() {
		t.Fatal("redirect target received a request")
	}
}

func TestNewClientValidationAndDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		config  Config
	}{
		{name: "invalid URL", address: "://", config: testConfig(http.DefaultClient)},
		{name: "unsupported scheme", address: "ftp://example.com", config: testConfig(http.DefaultClient)},
		{name: "missing host", address: "https:///path", config: testConfig(http.DefaultClient)},
		{name: "nil HTTP client", address: "https://example.com", config: Config{DefaultBranch: "main", UserAgent: "test"}},
		{name: "empty branch", address: "https://example.com", config: Config{HTTPClient: http.DefaultClient, UserAgent: "test"}},
		{name: "empty user agent", address: "https://example.com", config: Config{HTTPClient: http.DefaultClient, DefaultBranch: "main"}},
		{name: "negative response limit", address: "https://example.com", config: Config{HTTPClient: http.DefaultClient, DefaultBranch: "main", UserAgent: "test", MaxBodyBytes: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(tt.address, tt.config); err == nil {
				t.Fatal("NewClient() error = nil")
			}
		})
	}
	client, err := NewClient("https://example.com/base/?discarded=yes#fragment", testConfig(http.DefaultClient))
	if err != nil {
		t.Fatal(err)
	}
	if client.DefaultBranch() != "main" || client.maxBodyBytes != defaultMaxBodyBytes {
		t.Fatalf("client defaults = branch %q, limit %d", client.DefaultBranch(), client.maxBodyBytes)
	}
}

func TestEndpointConstruction(t *testing.T) {
	t.Parallel()
	client, err := NewClient("https://example.com/root/", testConfig(http.DefaultClient))
	if err != nil {
		t.Fatal(err)
	}
	endpoint := client.Endpoint("/api/schema", url.Values{"branch": {"feature/a b"}})
	if endpoint.Path != "/root/api/schema" || endpoint.Query().Get("branch") != "feature/a b" {
		t.Fatalf("Endpoint() = %s", endpoint)
	}
	endpoint = client.EndpointSegments([]string{"api", ".", "..", "a/b"}, url.Values{"x": {"1"}})
	if got, want := endpoint.EscapedPath(), "/root/api/%2E/%2E%2E/a%2Fb"; got != want {
		t.Fatalf("escaped path = %q, want %q", got, want)
	}
	if endpoint.Query().Get("x") != "1" {
		t.Fatalf("query = %q", endpoint.RawQuery)
	}
}

func TestExecuteValidationAndDecodeErrors(t *testing.T) {
	t.Parallel()
	responses := make(chan string, 4)
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(<-responses))}, nil
	})}
	client, err := NewClient("https://example.com", testConfig(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "  "}, nil); err == nil {
		t.Fatal("empty query error = nil")
	}
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "query { x }", Variables: map[string]any{"bad": make(chan int)}}, nil); err == nil || !strings.Contains(err.Error(), "encode GraphQL request") {
		t.Fatalf("encode error = %v", err)
	}
	responses <- "not-json"
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil); err == nil || !strings.Contains(err.Error(), "decode GraphQL response") {
		t.Fatalf("response error = %v", err)
	}
	responses <- `{"data":{"value":"text"}}`
	var data struct {
		Value int `json:"value"`
	}
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, &data); err == nil || !strings.Contains(err.Error(), "decode GraphQL data") {
		t.Fatalf("data error = %v", err)
	}
	responses <- `{"errors":[]}`
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil); err == nil || !strings.Contains(err.Error(), "missing data") {
		t.Fatalf("missing data error = %v", err)
	}
	responses <- `{"data":null}`
	if err := client.Execute(context.Background(), GraphQLRequest{Query: "query { x }"}, nil); err != nil {
		t.Fatalf("null data error = %v", err)
	}
}

func TestDoResponseHeadersAndReadFailure(t *testing.T) {
	t.Parallel()
	var received http.Header
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received = request.Header.Clone()
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"X-Result": {"created"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})}
	config := testConfig(httpClient)
	config.Headers = http.Header{"X-Global": {"one", "two"}}
	client, err := NewClient("https://example.com", config)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.DoResponse(context.Background(), http.MethodPost, client.Endpoint("api/test", nil), strings.NewReader("{}"), http.Header{"X-Request": {"value"}}, "tracker")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || response.Header.Get("X-Result") != "created" || string(response.Body) != `{"ok":true}` {
		t.Fatalf("response = %#v", response)
	}
	if received.Get("Content-Type") != "application/json" || received.Get("Accept") != "application/json" || received.Get("X-Request") != "value" || len(received.Values("X-Global")) != 2 || received.Get("X-Infrahub-Tracker") != "tracker" {
		t.Fatalf("headers = %#v", received)
	}

	sentinel := errors.New("read failed")
	httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: failingBody{err: sentinel}}, nil
	})
	if _, err := client.DoResponse(context.Background(), http.MethodGet, client.Endpoint("api/test", nil), nil, nil, ""); !errors.Is(err, sentinel) {
		t.Fatalf("read error = %v", err)
	}
}

func TestProtocolErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want string
	}{
		{err: &HTTPError{StatusCode: 404, Method: http.MethodGet, URL: "https://example.com"}, want: "returned HTTP 404"},
		{err: &GraphQLError{Items: []GraphQLErrorItem{{Message: "first"}, {Message: "second"}}}, want: "first | second"},
		{err: &OperationError{Operation: "Create"}, want: "operation Create did not succeed"},
		{err: &NotFoundError{Kind: "InfraDevice", Identifier: "edge-01"}, want: `InfraDevice "edge-01" not found`},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); !strings.Contains(got, tt.want) {
			t.Errorf("Error() = %q, want substring %q", got, tt.want)
		}
	}
}
