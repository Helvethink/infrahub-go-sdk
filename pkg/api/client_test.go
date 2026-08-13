package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
)

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
