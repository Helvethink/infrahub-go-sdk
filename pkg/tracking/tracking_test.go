package tracking_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}

func TestGroupCollectsUniqueMembersConcurrently(t *testing.T) {
	t.Parallel()
	group, err := tracking.NewGroup(tracking.GroupOptions{Identifier: "generator", Params: map[string]string{"site": "zrh"}})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			group.RecordNodeIDs("node-b", "node-a", "node-a", "")
		}()
	}
	workers.Wait()
	if got, want := group.Members(), []string{"node-a", "node-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Members() = %#v, want %#v", got, want)
	}
	firstName, secondName := group.Name(), group.Name()
	if !strings.HasPrefix(firstName, "generator-") || firstName != secondName {
		t.Fatalf("Name() = %q then %q", firstName, secondName)
	}
}

func TestSaveUsesVariablesAndUpsertsGroup(t *testing.T) {
	t.Parallel()
	group, err := tracking.NewGroup(tracking.GroupOptions{
		Identifier: "generator", Description: "generated objects", Branch: "feature/groups",
		GroupFields: map[string]any{"group_type": map[string]any{"value": "internal"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	group.RecordNodeIDs("node-b", "node-a")
	client := executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName != "CoreStandardGroupUpsert" || request.Branch != "feature/groups" {
			t.Fatalf("request = %#v", request)
		}
		if !strings.Contains(request.Query, "kind: __typename") {
			t.Fatalf("tracking group kind must use a __typename alias: %s", request.Query)
		}
		if strings.Contains(request.Query, "node-a") || !strings.Contains(request.Query, "$data: CoreStandardGroupUpsertInput!") {
			t.Fatalf("query = %s", request.Query)
		}
		data, ok := request.Variables["data"].(map[string]any)
		if !ok {
			t.Fatalf("data = %T", request.Variables["data"])
		}
		members, ok := data["members"].([]map[string]string)
		if !ok || !reflect.DeepEqual(members, []map[string]string{{"id": "node-a"}, {"id": "node-b"}}) {
			t.Fatalf("members = %#v", data["members"])
		}
		return json.Unmarshal([]byte(`{"CoreStandardGroupUpsert":{"ok":true,"object":{"id":"group-id","kind":"CoreStandardGroup","display_label":"generator"}}}`), dst)
	})
	result, err := group.Save(context.Background(), client)
	if err != nil || result == nil || result.ID != "group-id" {
		t.Fatalf("Save() = %#v, %v", result, err)
	}
}

func TestSaveWithoutMembersIsNoOp(t *testing.T) {
	t.Parallel()
	group, err := tracking.NewGroup(tracking.GroupOptions{Identifier: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := group.Save(context.Background(), nil)
	if err != nil || result != nil {
		t.Fatalf("Save() = %#v, %v", result, err)
	}
}

func TestSaveReturnsOperationError(t *testing.T) {
	t.Parallel()
	group, err := tracking.NewGroup(tracking.GroupOptions{Identifier: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	group.RecordNodeIDs("node")
	_, err = group.Save(context.Background(), executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
		return json.Unmarshal([]byte(`{"CoreStandardGroupUpsert":{"ok":false,"object":null}}`), dst)
	}))
	var operationError *api.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestNewGroupValidatesInput(t *testing.T) {
	t.Parallel()
	tests := []tracking.GroupOptions{
		{},
		{Identifier: "valid", GroupKind: "Kind) { secret"},
		{Identifier: "valid", GroupFields: map[string]any{"members": []string{"override"}}},
	}
	for _, options := range tests {
		if _, err := tracking.NewGroup(options); err == nil {
			t.Fatalf("NewGroup(%#v) error = nil", options)
		}
	}
}
