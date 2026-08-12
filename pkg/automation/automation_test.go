package automation_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/automation"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
)

func newService(t *testing.T, handler http.HandlerFunc) (*automation.Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := api.NewClient(server.URL+"/base", api.Config{HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test"})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return automation.NewService(client), server
}

func TestQueryEncodesPathParametersVariablesAndTracker(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	service, server := newService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/base/api/query/device%2Fconfig" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if r.URL.Query().Get("branch") != "feature/a" || r.URL.Query().Get("at") != at.Format(time.RFC3339Nano) || r.URL.Query().Get("update_group") != "true" || !reflect.DeepEqual(r.URL.Query()["subscribers"], []string{"one", "two"}) {
			t.Errorf("query = %#v", r.URL.Query())
		}
		if r.Header.Get("X-Infrahub-Tracker") != "workflow" {
			t.Errorf("tracker = %q", r.Header.Get("X-Infrahub-Tracker"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["variables"].(map[string]any)["device"] != "edge-01" {
			t.Errorf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"data":{"Device":{"count":1}}}`))
	})
	defer server.Close()
	var result map[string]any
	err := service.Query(tracking.WithTracker(context.Background(), "workflow"), automation.QueryOptions{Name: "device/config", Variables: map[string]any{"device": "edge-01"}, Branch: "feature/a", At: at, UpdateGroup: true, Subscribers: []string{"one", "two"}}, &result)
	if err != nil || result["data"] == nil {
		t.Fatalf("Query() = %#v, %v", result, err)
	}
}

func TestRunTransformUnpacksData(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":{"value":21}}`)) })
	defer server.Close()
	result, err := service.RunTransform(context.Background(), automation.RunOptions{Query: automation.QueryOptions{Name: "double"}}, func(_ context.Context, data map[string]any) (any, error) {
		return int(data["value"].(float64)) * 2, nil
	})
	if err != nil || result != 42 {
		t.Fatalf("RunTransform() = %#v, %v", result, err)
	}
}

func TestRunGeneratorPropagatesContextAndError(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":{"items":[]}}`)) })
	defer server.Close()
	expected := errors.New("generate failed")
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "present")
	err := service.RunGenerator(ctx, automation.RunOptions{Query: automation.QueryOptions{Name: "generator"}}, func(ctx context.Context, _ map[string]any) error {
		if ctx.Value(contextKey{}) != "present" {
			t.Fatal("context value was lost")
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestRunCheckProducesStructuredResult(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"invalid":["node-2"]}}`))
	})
	defer server.Close()
	result, err := service.RunCheck(context.Background(), automation.RunOptions{Query: automation.QueryOptions{Name: "check", Branch: "main"}}, func(_ context.Context, _ map[string]any, reporter *automation.Reporter) error {
		reporter.Warning("review", "node-1", "Device")
		reporter.Error("invalid", "node-2", "Device")
		return nil
	})
	if err != nil || result.Passed || len(result.Findings) != 2 || result.Findings[1].Branch != "main" {
		t.Fatalf("RunCheck() = %#v, %v", result, err)
	}
}

func TestSuccessfulCheckAddsCompletionFinding(t *testing.T) {
	t.Parallel()
	service, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":{}}`)) })
	defer server.Close()
	result, err := service.RunCheck(context.Background(), automation.RunOptions{Query: automation.QueryOptions{Name: "check"}}, func(_ context.Context, _ map[string]any, reporter *automation.Reporter) error {
		reporter.Info("started", "", "")
		return nil
	})
	if err != nil || !result.Passed || len(result.Findings) != 2 || result.Findings[1].Message != "Check successfully completed" {
		t.Fatalf("RunCheck() = %#v, %v", result, err)
	}
}

func TestReporterIsConcurrencySafe(t *testing.T) {
	t.Parallel()
	reporter := automation.NewReporter("main")
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() { defer workers.Done(); reporter.Info("message", "", "") }()
	}
	workers.Wait()
	findings := reporter.Findings()
	if len(findings) != 20 {
		t.Fatalf("findings = %d", len(findings))
	}
	for index, finding := range findings {
		if finding.Sequence != index {
			t.Fatalf("sequence[%d] = %d", index, finding.Sequence)
		}
	}
}

func TestValidationAndHTTPError(t *testing.T) {
	t.Parallel()
	service := automation.NewService(nil)
	if err := service.Query(context.Background(), automation.QueryOptions{}, &map[string]any{}); err == nil {
		t.Fatal("empty name error = nil")
	}
	if err := service.Query(context.Background(), automation.QueryOptions{Name: "query"}, nil); err == nil {
		t.Fatal("nil destination error = nil")
	}
	if _, err := service.RunTransform(context.Background(), automation.RunOptions{}, nil); err == nil {
		t.Fatal("nil transform error = nil")
	}
	if err := service.RunGenerator(context.Background(), automation.RunOptions{}, nil); err == nil {
		t.Fatal("nil generator error = nil")
	}
	if _, err := service.RunCheck(context.Background(), automation.RunOptions{}, nil); err == nil {
		t.Fatal("nil check error = nil")
	}

	httpService, server := newService(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	defer server.Close()
	err := httpService.Query(context.Background(), automation.QueryOptions{Name: "denied"}, &map[string]any{})
	var httpError *api.HTTPError
	if !errors.As(err, &httpError) || httpError.StatusCode != http.StatusForbidden {
		t.Fatalf("error = %T %v", err, err)
	}
}
