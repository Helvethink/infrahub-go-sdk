package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

type payload struct {
	Query         string `json:"query"`
	OperationName string `json:"operationName"`
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

func TestCreateAndDeleteUseGeneratedInputTypes(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.OperationName == "BuiltinTagCreate" {
			if !strings.Contains(request.Query, "$data: BuiltinTagCreateInput!") {
				t.Errorf("query = %s", request.Query)
			}
			_, _ = w.Write([]byte(`{"data":{"BuiltinTagCreate":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}}}`))
			return
		}
		if !strings.Contains(request.Query, "$data: DeleteInput!") {
			t.Errorf("query = %s", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTagDelete":{"ok":true}}}`))
	})
	defer server.Close()
	created, err := service.Create(context.Background(), "BuiltinTag", map[string]any{"name": map[string]any{"value": "staging"}}, "")
	if err != nil || created.ID != "tag-id" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	if err := service.Delete(context.Background(), "BuiltinTag", map[string]any{"id": "tag-id"}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestListGetAndNotFound(t *testing.T) {
	t.Parallel()
	var found atomic.Bool
	found.Store(true)
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if found.Load() {
			_, _ = w.Write([]byte(`{"data":{"BuiltinTag":{"count":1,"edges":[{"node":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTag":{"count":0,"edges":[]}}}`))
	})
	defer server.Close()
	page, err := service.List(context.Background(), "BuiltinTag", 0, 100, "main")
	if err != nil || page.Count != 1 || len(page.Nodes) != 1 {
		t.Fatalf("List() = %#v, %v", page, err)
	}
	n, err := service.GetByHFID(context.Background(), "BuiltinTag", []string{"staging"}, "main")
	if err != nil || n.ID != "tag-id" {
		t.Fatalf("GetByHFID() = %#v, %v", n, err)
	}
	found.Store(false)
	_, err = service.GetByID(context.Background(), "BuiltinTag", "missing", "")
	var notFound *api.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestRejectsGraphQLInjectionInKind(t *testing.T) {
	t.Parallel()
	service := NewService(nil)
	_, err := service.Create(context.Background(), "Tag) { secret }", nil, "")
	if err == nil {
		t.Fatal("Create() error = nil")
	}
}
