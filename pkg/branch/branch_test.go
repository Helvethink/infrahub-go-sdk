package branch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (fn executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return fn(ctx, request, dst)
}

func decodeResult(t *testing.T, data string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), dst); err != nil {
		t.Fatal(err)
	}
}

type payload struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

func newTestService(t *testing.T, handler http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := api.NewClient(server.URL, api.Config{
		HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test",
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return NewService(client), server
}

func TestListAndCreate(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		switch request.OperationName {
		case "BranchList":
			_, _ = w.Write([]byte(`{"data":{"Branch":[{"id":"1","name":"main","branched_from":"x","is_default":true,"sync_with_git":false,"has_schema_changes":false,"status":"OPEN"}]}}`))
		case "BranchCreate":
			if request.Variables["data"].(map[string]any)["name"] != "work" {
				t.Errorf("variables = %#v", request.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"BranchCreate":{"ok":true,"object":{"id":"2","name":"work","branched_from":"x","is_default":false,"sync_with_git":false,"has_schema_changes":false,"status":"OPEN"}}}}`))
		default:
			t.Errorf("operation = %q", request.OperationName)
		}
	})
	defer server.Close()
	branches, err := service.List(context.Background())
	if err != nil || len(branches) != 1 || branches[0].Name != "main" {
		t.Fatalf("List() = %#v, %v", branches, err)
	}
	created, err := service.Create(context.Background(), "work", CreateOptions{})
	if err != nil || created.Name != "work" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
}

func TestMutationsUseServerInputTypes(t *testing.T) {
	t.Parallel()
	wantTypes := map[string]string{
		"BranchDelete": "BranchDeleteInput!", "BranchRebase": "BranchNameInput!",
		"BranchValidate": "BranchNameInput!", "BranchMerge": "BranchNameInput!",
	}
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if !strings.Contains(request.Query, wantTypes[request.OperationName]) {
			t.Errorf("query for %s = %s", request.OperationName, request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"` + request.OperationName + `":{"ok":true}}}`))
	})
	defer server.Close()
	operations := []func(context.Context, string) error{service.Delete, service.Rebase, service.Validate, service.Merge}
	for _, operation := range operations {
		if err := operation(context.Background(), "work"); err != nil {
			t.Error(err)
		}
	}
}

func TestDiffDataUsesDiffTreeGraphQL(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/graphql/feature%2Fone" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.OperationName != "BranchDiffData" || !strings.Contains(request.Query, "DiffTree(") {
			t.Errorf("request = %#v", request)
		}
		if request.Variables["branch"] != "feature/one" || request.Variables["includeParents"] != false || request.Variables["fromTime"] != "2026-08-01T00:00:00Z" || request.Variables["toTime"] != nil {
			t.Errorf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"DiffTree":{"num_added":1,"diff_branch":"feature/one","nodes":[]}}}`))
	})
	defer server.Close()

	var result map[string]any
	if err := service.DiffData(context.Background(), "feature/one", true, "2026-08-01T00:00:00Z", "", &result); err != nil {
		t.Fatal(err)
	}
	if result["diff_branch"] != "feature/one" || result["num_added"] != float64(1) {
		t.Fatalf("result = %#v", result)
	}
}

func TestDiffDataValidatesArguments(t *testing.T) {
	t.Parallel()
	service := NewService(nil)
	if err := service.DiffData(context.Background(), "", false, "", "", &map[string]any{}); err == nil {
		t.Fatal("empty branch error = nil")
	}
	if err := service.DiffData(context.Background(), "feature", false, "", "", nil); err == nil {
		t.Fatal("nil destination error = nil")
	}
}

func TestGetSuccessNotFoundAndTransportError(t *testing.T) {
	t.Parallel()
	service := NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName != "BranchGet" || request.Variables["name"] != "work" {
			t.Fatalf("request = %#v", request)
		}
		decodeResult(t, `{"Branch":[{"id":"branch-id","name":"work","status":"OPEN"}]}`, dst)
		return nil
	}))
	branch, err := service.Get(context.Background(), "work")
	if err != nil || branch.Name != "work" {
		t.Fatalf("Get() = %#v, %v", branch, err)
	}
	service = NewService(executorFunc(func(context.Context, api.GraphQLRequest, any) error { return nil }))
	if _, err := service.Get(context.Background(), "missing"); err == nil {
		t.Fatal("Get() not-found error = nil")
	}
	sentinel := errors.New("transport failed")
	service = NewService(executorFunc(func(context.Context, api.GraphQLRequest, any) error { return sentinel }))
	if _, err := service.Get(context.Background(), "work"); !errors.Is(err, sentinel) {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestCreateAndSimpleMutationFailures(t *testing.T) {
	t.Parallel()
	service := NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		switch request.OperationName {
		case "BranchCreate":
			decodeResult(t, `{"BranchCreate":{"ok":false,"object":null}}`, dst)
		case "BranchMerge":
			decodeResult(t, `{"BranchMerge":{"ok":false}}`, dst)
		}
		return nil
	}))
	if _, err := service.Create(context.Background(), "work", CreateOptions{}); err == nil {
		t.Fatal("Create() operation error = nil")
	}
	if err := service.Merge(context.Background(), "work"); err == nil {
		t.Fatal("Merge() operation error = nil")
	}
	sentinel := errors.New("GraphQL failed")
	service = NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName == "BranchCreate" {
			decodeResult(t, `{"BranchCreate":{"ok":true,"object":{"id":"branch-id","name":"work"}}}`, dst)
		}
		return sentinel
	}))
	created, err := service.Create(context.Background(), "work", CreateOptions{})
	if !errors.Is(err, sentinel) || created == nil || created.ID != "branch-id" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	if err := service.Delete(context.Background(), "work"); !errors.Is(err, sentinel) {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestDiffDataResponseEdges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		err      error
		wantErr  bool
	}{
		{name: "null tree", response: `{"DiffTree":null}`},
		{name: "malformed destination", response: `{"DiffTree":"not-an-object"}`, wantErr: true},
		{name: "transport", err: errors.New("transport failed"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
				if tt.response != "" {
					decodeResult(t, tt.response, dst)
				}
				return tt.err
			}))
			var result map[string]any
			err := service.DiffData(context.Background(), "work", false, "", "", &result)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DiffData() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
