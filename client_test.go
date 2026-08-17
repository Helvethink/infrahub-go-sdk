package infrahub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewClientValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		address string
		options []Option
	}{
		{name: "relative address", address: "/infrahub"},
		{name: "unsupported scheme", address: "ftp://example.com"},
		{name: "nil option", address: "https://example.com", options: []Option{nil}},
		{name: "nil HTTP client", address: "https://example.com", options: []Option{WithHTTPClient(nil)}},
		{name: "empty branch", address: "https://example.com", options: []Option{WithDefaultBranch("")}},
		{name: "empty user agent", address: "https://example.com", options: []Option{WithUserAgent("")}},
		{name: "empty header name", address: "https://example.com", options: []Option{WithHeader("", "value")}},
		{name: "auth header", address: "https://example.com", options: []Option{WithHeader("Authorization", "secret")}},
		{name: "Infrahub auth header", address: "https://example.com", options: []Option{WithHeader("X-INFRAHUB-KEY", "secret")}},
		{name: "invalid limit", address: "https://example.com", options: []Option{WithMaxResponseBytes(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(tt.address, tt.options...); err == nil {
				t.Fatal("NewClient() error = nil")
			}
		})
	}
}

func TestNewClientWiresServices(t *testing.T) {
	t.Parallel()
	client, err := NewClient("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if client.Automation == nil || client.Branches == nil || client.Diffs == nil || client.Nodes == nil || client.ObjectStore == nil || client.Repositories == nil || client.ResourcePools == nil || client.Schema == nil || client.Tasks == nil || client.Telemetry == nil || client.Traversal == nil {
		t.Fatalf("services not initialized: %#v", client)
	}
}

func TestClientOptionsAndCompatibilityHelpers(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "custom-agent" || r.Header.Get("X-Custom") != "one" || r.Header.Get("X-INFRAHUB-KEY") != "secret" {
			t.Errorf("headers = %#v", r.Header)
		}
		if r.Header.Get("X-Infrahub-Tracker") != "workflow" {
			t.Errorf("tracker = %q", r.Header.Get("X-Infrahub-Tracker"))
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL,
		WithHTTPClient(server.Client()),
		WithAPIToken("secret"),
		WithDefaultBranch("develop"),
		WithUserAgent("custom-agent"),
		WithHeader("X-Custom", "one"),
		WithMaxResponseBytes(1024),
	)
	if err != nil {
		t.Fatal(err)
	}
	if client.DefaultBranch() != "develop" {
		t.Fatalf("default branch = %q", client.DefaultBranch())
	}
	if err := client.Execute(WithTracker(context.Background(), "workflow"), GraphQLRequest{Query: "query { ok }"}, nil); err != nil {
		t.Fatal(err)
	}
	group, err := NewTrackingGroup(TrackingGroupOptions{Identifier: "workflow"})
	if err != nil || group.Name() != "workflow" {
		t.Fatalf("group=%#v err=%v", group, err)
	}
}

func TestDefaultClientRefusesRedirects(t *testing.T) {
	t.Parallel()
	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalled.Store(true)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithAPIToken("do-not-forward"))
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
