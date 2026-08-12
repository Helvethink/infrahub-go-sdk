package diff_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/diff"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}
func decode(dst any, payload string) error { return json.Unmarshal([]byte(payload), dst) }

const treePayload = `{"DiffTree":{"num_added":1,"num_updated":2,"num_removed":3,"num_conflicts":4,"num_untracked_base_changes":5,"num_untracked_diff_changes":6,"to_time":"2025-11-14T18:00:00Z","from_time":"2025-11-14T12:00:00Z","base_branch":"main","diff_branch":"feature","name":"named","nodes":[{"uuid":"node-id","kind":"TestPerson","status":"UPDATED","label":"Jane","num_added":0,"num_updated":2,"num_removed":0,"attributes":[{"name":"name","status":"UPDATED","num_added":0,"num_updated":1,"num_removed":0}],"relationships":[{"name":"cars","status":"UPDATED","cardinality":"MANY","num_added":1,"num_updated":0,"num_removed":0,"elements":[{"status":"ADDED","num_added":1,"num_updated":0,"num_removed":0}]}]}]}}`

func TestTreeUsesVariablesAndConvertsNodes(t *testing.T) {
	t.Parallel()
	from := time.Date(2025, 11, 14, 12, 0, 0, 0, time.UTC)
	to := from.Add(6 * time.Hour)
	service := diff.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.Branch != "feature" || request.OperationName != "GetDiffTree" {
			t.Fatalf("request = %#v", request)
		}
		if strings.Contains(request.Query, "named") || !strings.Contains(request.Query, "$fromTime: DateTime") {
			t.Fatalf("query = %s", request.Query)
		}
		if request.Variables["name"] != "named" || request.Variables["fromTime"] != from || request.Variables["toTime"] != to {
			t.Fatalf("variables = %#v", request.Variables)
		}
		return decode(dst, treePayload)
	}))
	tree, err := service.Tree(context.Background(), diff.Options{Branch: "feature", Name: "named", FromTime: from, ToTime: to})
	if err != nil || tree == nil || tree.Added != 1 || tree.UntrackedDiffChanges != 6 || len(tree.Nodes) != 1 {
		t.Fatalf("Tree() = %#v, %v", tree, err)
	}
	node := tree.Nodes[0]
	if node.Branch != "feature" || node.Summary.Updated != 2 || len(node.Elements) != 2 {
		t.Fatalf("node = %#v", node)
	}
	if node.Elements[0].Type != diff.ElementTypeAttribute || node.Elements[1].Type != diff.ElementTypeRelationshipMany || len(node.Elements[1].Peers) != 1 {
		t.Fatalf("elements = %#v", node.Elements)
	}
}

func TestSummaryUsesMinimalSelectionAndHandlesMissing(t *testing.T) {
	t.Parallel()
	missing := false
	service := diff.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if strings.Contains(request.Query, "base_branch") {
			t.Fatalf("summary requested metadata: %s", request.Query)
		}
		if missing {
			return decode(dst, `{"DiffTree":null}`)
		}
		return decode(dst, treePayload)
	}))
	nodes, err := service.Summary(context.Background(), diff.Options{Branch: "feature"})
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Summary() = %#v, %v", nodes, err)
	}
	missing = true
	nodes, err = service.Summary(context.Background(), diff.Options{Branch: "feature"})
	if err != nil || nodes == nil || len(nodes) != 0 {
		t.Fatalf("missing Summary() = %#v, %v", nodes, err)
	}
}

func TestTreeReturnsPartialDataAndGraphQLError(t *testing.T) {
	t.Parallel()
	gqlErr := &api.GraphQLError{Items: []api.GraphQLErrorItem{{Message: "partial"}}}
	service := diff.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
		if err := decode(dst, treePayload); err != nil {
			return err
		}
		return gqlErr
	}))
	tree, err := service.Tree(context.Background(), diff.Options{Branch: "feature"})
	var target *api.GraphQLError
	if tree == nil || !errors.As(err, &target) {
		t.Fatalf("Tree() = %#v, %T %v", tree, err, err)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	service := diff.NewService(nil)
	if _, err := service.Tree(context.Background(), diff.Options{}); err == nil {
		t.Fatal("empty branch error = nil")
	}
	to := time.Now()
	if _, err := service.Tree(context.Background(), diff.Options{Branch: "feature", FromTime: to.Add(time.Hour), ToTime: to}); err == nil {
		t.Fatal("invalid range error = nil")
	}
}
