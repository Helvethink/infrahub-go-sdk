package branch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/api"
)

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
