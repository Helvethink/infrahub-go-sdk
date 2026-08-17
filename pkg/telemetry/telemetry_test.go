package telemetry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/telemetry"
)

func TestListEncodesFiltersAndDecodesSnapshots(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/root/api/telemetry/snapshots" || r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("offset") != "2" {
			t.Fatalf("URL = %s", r.URL.String())
		}
		if r.URL.Query().Get("start_date") != "2026-08-01T00:00:00Z" {
			t.Fatalf("start_date = %q", r.URL.Query().Get("start_date"))
		}
		_, _ = w.Write([]byte(`{"snapshots":[{"kind":"usage","deployment_id":"one"}],"count":1}`))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL+"/root", api.Config{HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	page, err := telemetry.NewService(client).List(context.Background(), telemetry.ListOptions{StartDate: start, Offset: 2, Limit: 50})
	if err != nil || page.Count != 1 || page.Snapshots[0]["deployment_id"] != "one" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestListValidation(t *testing.T) {
	t.Parallel()
	service := telemetry.NewService(nil)
	if _, err := service.List(context.Background(), telemetry.ListOptions{Offset: -1}); err == nil {
		t.Fatal("expected pagination error")
	}
	if _, err := service.List(context.Background(), telemetry.ListOptions{
		StartDate: time.Now(), EndDate: time.Now().Add(-time.Hour),
	}); err == nil {
		t.Fatal("expected date range error")
	}
}

func TestAllPaginates(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"snapshots":[` + snapshotsJSON(1000) + `],"count":1001}`))
			return
		}
		if r.URL.Query().Get("offset") != "1000" {
			t.Fatalf("offset = %q", r.URL.Query().Get("offset"))
		}
		_, _ = w.Write([]byte(`{"snapshots":[{"id":"last"}],"count":1001}`))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL, api.Config{HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := telemetry.NewService(client).All(context.Background(), telemetry.ListOptions{})
	if err != nil || len(items) != 1001 || requests != 2 {
		t.Fatalf("len=%d requests=%d err=%v", len(items), requests, err)
	}
}

func snapshotsJSON(count int) string {
	result := ""
	for index := 0; index < count; index++ {
		if index > 0 {
			result += ","
		}
		result += `{"id":"item"}`
	}
	return result
}

func TestListNormalizesEmptySnapshotsAndDecodeError(t *testing.T) {
	t.Parallel()
	responses := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(<-responses))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL, api.Config{HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	service := telemetry.NewService(client)
	responses <- `{"count":0}`
	page, err := service.List(context.Background(), telemetry.ListOptions{})
	if err != nil || page.Snapshots == nil || len(page.Snapshots) != 0 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	responses <- "not-json"
	if _, err := service.List(context.Background(), telemetry.ListOptions{}); err == nil {
		t.Fatal("decode error = nil")
	}
}

func TestAllPropagatesListError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL, api.Config{HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = telemetry.NewService(client).All(context.Background(), telemetry.ListOptions{})
	var httpError *api.HTTPError
	if !errors.As(err, &httpError) || httpError.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %T %v", err, err)
	}
}
