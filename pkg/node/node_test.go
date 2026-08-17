package node

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
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

func TestUpsertUsesGeneratedInputType(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.OperationName != "BuiltinTagUpsert" {
			t.Errorf("operation = %q", request.OperationName)
		}
		if !strings.Contains(request.Query, "$data: BuiltinTagUpsertInput!") {
			t.Errorf("query = %s", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTagUpsert":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}}}`))
	})
	defer server.Close()
	upserted, err := service.Upsert(context.Background(), "BuiltinTag", map[string]any{"name": map[string]any{"value": "staging"}}, "")
	if err != nil || upserted.ID != "tag-id" {
		t.Fatalf("Upsert() = %#v, %v", upserted, err)
	}
}

func TestListGetAndNotFound(t *testing.T) {
	t.Parallel()
	var found atomic.Bool
	found.Store(true)
	service, server := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
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

func TestQueryRecordsNodesInTrackingGroup(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"BuiltinTag":{"count":2,"edges":[{"node":{"id":"tag-b","kind":"BuiltinTag"}},{"node":{"id":"tag-a","kind":"BuiltinTag"}}]}}}`))
	})
	defer server.Close()
	group, err := tracking.NewGroup(tracking.GroupOptions{Identifier: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(group.Context(context.Background()), "BuiltinTag", 0, 10, "main"); err != nil {
		t.Fatal(err)
	}
	if got, want := group.Members(), []string{"tag-a", "tag-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Members() = %#v, want %#v", got, want)
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

func TestIdentityKindUsesTypenameAlias(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if !strings.Contains(request.Query, "kind: __typename") {
			t.Errorf("query = %s", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"CoreGraphQLQuery":{"count":1,"edges":[{"node":{"id":"query-id","kind":"CoreGraphQLQuery","hfid":["query"],"display_label":"query"}}]}}}`))
	})
	defer server.Close()
	page, err := service.List(context.Background(), "CoreGraphQLQuery", 0, 1, "main")
	if err != nil || len(page.Nodes) != 1 || page.Nodes[0].Kind != "CoreGraphQLQuery" {
		t.Fatalf("List() = %#v, %v", page, err)
	}
}

func TestQueryDynamicFiltersAndSelections(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		checks := []string{
			"$filter0: String!", "$filter1: [ID!]!", "name__value: $filter0",
			"member_of_groups__ids: $filter1", "name { value }",
			"site { node { id display_label } }",
		}
		for _, expected := range checks {
			if !strings.Contains(request.Query, expected) {
				t.Errorf("query does not contain %q: %s", expected, request.Query)
			}
		}
		if strings.Contains(request.Query, "staging") {
			t.Errorf("filter value was interpolated: %s", request.Query)
		}
		if request.Variables["filter0"] != "staging" {
			t.Errorf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTag":{"count":1,"edges":[{"node":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging","name":{"value":"staging"},"site":{"node":{"id":"site-id","display_label":"Paris"}}}}]}}}`))
	})
	defer server.Close()
	page, err := service.Query(context.Background(), "BuiltinTag", QueryOptions{
		Branch: "main", Offset: 10, Limit: 25,
		Filters: []Filter{
			{Name: "name__value", Value: "staging"},
			{Name: "member_of_groups__ids", Value: []string{"group-id"}, Type: "[ID!]!"},
		},
		Selections: []Selection{
			Select("name", Select("value")),
			Select("site", Select("node", Select("id"), Select("display_label"))),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 1 || page.Offset != 10 || page.Limit != 25 {
		t.Fatalf("page = %#v", page)
	}
	name, ok := page.Nodes[0].Fields["name"].(map[string]any)
	if !ok || name["value"] != "staging" {
		t.Fatalf("dynamic fields = %#v", page.Nodes[0].Fields)
	}
}

func TestQuerySupportsExplicitInfrahubScalar(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if !strings.Contains(request.Query, "$filter0: BigInt!") {
			t.Errorf("query = %s", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinIPPrefix":{"count":0,"edges":[]}}}`))
	})
	defer server.Close()
	_, err := service.Query(context.Background(), "BuiltinIPPrefix", QueryOptions{
		Filters: []Filter{{Name: "utilization__value", Value: int64(5_000_000_000), Type: "BigInt!"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestQueryRejectsUnsafeDocumentParts(t *testing.T) {
	t.Parallel()
	service := NewService(nil)
	tests := []struct {
		name    string
		options QueryOptions
	}{
		{name: "filter name", options: QueryOptions{Filters: []Filter{{Name: "name) { secret", Value: "x"}}}},
		{name: "filter type", options: QueryOptions{Filters: []Filter{{Name: "name__value", Value: "x", Type: "String!) { secret"}}}},
		{name: "selection", options: QueryOptions{Selections: []Selection{Select("name { secret")}}},
		{name: "nested duplicate", options: QueryOptions{Selections: []Selection{Select("name", Select("value"), Select("value"))}}},
		{name: "top-level duplicate", options: QueryOptions{Selections: []Selection{Select("name"), Select("name")}}},
		{name: "duplicate filter", options: QueryOptions{Filters: []Filter{{Name: "name__value", Value: "a"}, {Name: "name__value", Value: "b"}}}},
		{
			name: "unknown value type",
			options: QueryOptions{
				Filters: []Filter{{Name: "name__value", Value: struct{}{}}},
			},
		},
		{name: "oversized int", options: QueryOptions{Filters: []Filter{{Name: "number__value", Value: int64(5_000_000_000)}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := service.Query(context.Background(), "BuiltinTag", test.options); err == nil {
				t.Fatal("Query() error = nil")
			}
		})
	}
}

func TestGraphQLTypeValidation(t *testing.T) {
	t.Parallel()
	valid := []string{"String", "String!", "[ID!]", "[ID!]!", "[[String!]!]!", "InfrahubCustomScalar"}
	for _, value := range valid {
		if !isGraphQLType(value) {
			t.Errorf("isGraphQLType(%q) = false", value)
		}
	}
	invalid := []string{"", "1String", "String!!", "[String", "String]", "[String]! extra", "String) { secret"}
	for _, value := range invalid {
		if isGraphQLType(value) {
			t.Errorf("isGraphQLType(%q) = true", value)
		}
	}
}

func TestInferGraphQLTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   any
		want    string
		wantErr bool
	}{
		{name: "string", value: "value", want: "String!"},
		{name: "boolean", value: true, want: "Boolean!"},
		{name: "int", value: int(1), want: "Int!"},
		{name: "int8", value: int8(1), want: "Int!"},
		{name: "int16", value: int16(1), want: "Int!"},
		{name: "int32", value: int32(1), want: "Int!"},
		{name: "int64", value: int64(1), want: "Int!"},
		{name: "float32", value: float32(1.5), want: "Float!"},
		{name: "float64", value: 1.5, want: "Float!"},
		{name: "JSON integer", value: json.Number("42"), want: "Int!"},
		{name: "JSON float", value: json.Number("4.2"), want: "Float!"},
		{name: "invalid JSON number", value: json.Number("invalid"), wantErr: true},
		{name: "time", value: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), want: "DateTime!"},
		{name: "strings", value: []string{"one"}, want: "[String!]!"},
		{name: "booleans", value: []bool{true}, want: "[Boolean!]!"},
		{name: "integers", value: []int{1, 2}, want: "[Int!]!"},
		{name: "floats", value: []float64{1.5}, want: "[Float!]!"},
		{name: "oversized integer list", value: []int{math.MaxInt32 + 1}, wantErr: true},
		{name: "unsupported", value: struct{}{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := inferGraphQLType(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("inferGraphQLType(%T) error = nil", tt.value)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("inferGraphQLType(%T) = %q, %v; want %q", tt.value, got, err, tt.want)
			}
		})
	}
}

func TestUpdateAndOperationFailures(t *testing.T) {
	t.Parallel()
	service, server := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var request payload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request.OperationName {
		case "BuiltinTagUpdate":
			_, _ = w.Write([]byte(`{"data":{"BuiltinTagUpdate":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag"}}}}`))
		case "BuiltinTagCreate":
			_, _ = w.Write([]byte(`{"data":{"BuiltinTagCreate":{"ok":false,"object":null}}}`))
		case "BuiltinTagDelete":
			_, _ = w.Write([]byte(`{"data":{"BuiltinTagDelete":{"ok":false}}}`))
		}
	})
	defer server.Close()
	updated, err := service.Update(context.Background(), "BuiltinTag", map[string]any{"id": "tag-id"}, "main")
	if err != nil || updated.ID != "tag-id" {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	if _, err := service.Create(context.Background(), "BuiltinTag", nil, "main"); err == nil {
		t.Fatal("Create() operation error = nil")
	}
	if err := service.Delete(context.Background(), "BuiltinTag", nil, "main"); err == nil {
		t.Fatal("Delete() operation error = nil")
	}
	if _, err := service.GetByID(context.Background(), "BuiltinTag", "", "main"); err == nil {
		t.Fatal("GetByID() empty ID error = nil")
	}
	if _, err := service.GetByHFID(context.Background(), "BuiltinTag", nil, "main"); err == nil {
		t.Fatal("GetByHFID() empty HFID error = nil")
	}
}

func TestNodeUnmarshalRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	var value Node
	if err := json.Unmarshal([]byte("not-json"), &value); err == nil {
		t.Fatal("Unmarshal() error = nil")
	}
}
