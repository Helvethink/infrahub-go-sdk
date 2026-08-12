package traversal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/traversal"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}
func decode(dst any, payload string) error { return json.Unmarshal([]byte(payload), dst) }
func intPtr(value int) *int                { return &value }
func boolPtr(value bool) *bool             { return &value }

const pathsPayload = `{"InfrahubPathTraversal":{"paths":[{"depth":1,"hops":[{"node":{"id":"source","kind":"Device","label":"Device","display_label":"edge-01","hfid":["edge-01"]},"relationship":null},{"node":{"id":"destination","kind":"Site","label":"Site","display_label":"zrh","hfid":["zrh"]},"relationship":{"from_rel":"site","from_label":"Site","to_rel":"devices","to_label":"Devices","kind":"Generic"}}]}],"source":{"id":"source","kind":"Device","label":"Device","display_label":"edge-01","hfid":["edge-01"]},"destination":{"id":"destination","kind":"Site","label":"Site","display_label":"zrh","hfid":["zrh"]},"count":1,"excluded_kinds":["BuiltinIPNamespace"],"truncated_at_depth":null}}`

func TestPathsUsesInputVariableAndDecodesHops(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	service := traversal.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.Branch != "feature" || request.At != at || !strings.Contains(request.Query, "$data: PathTraversalInput!") {
			t.Fatalf("request = %#v", request)
		}
		if strings.Contains(request.Query, "source-id-value") {
			t.Fatalf("value interpolated: %s", request.Query)
		}
		data := request.Variables["data"].(map[string]any)
		if data["max_depth"] != 4 || data["shortest_paths_only"] != false || !reflect.DeepEqual(data["kind_filter"], []string{"Device"}) {
			t.Fatalf("data = %#v", data)
		}
		return decode(dst, pathsPayload)
	}))
	result, err := service.Paths(context.Background(), traversal.PathsOptions{SourceID: "source-id-value", DestinationID: "destination-id-value", MaxDepth: intPtr(4), KindFilter: []string{"Device"}, ShortestPathsOnly: boolPtr(false), Branch: "feature", At: at})
	if err != nil || result.Count != 1 || len(result.Paths[0].Hops) != 2 || result.Paths[0].Hops[0].Relationship != nil || result.Paths[0].Hops[1].Relationship.Kind != "Generic" {
		t.Fatalf("Paths() = %#v, %v", result, err)
	}
}

func TestPathExistsForcesOnePath(t *testing.T) {
	t.Parallel()
	service := traversal.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.Variables["data"].(map[string]any)["max_paths"] != 1 {
			t.Fatalf("data = %#v", request.Variables["data"])
		}
		return decode(dst, pathsPayload)
	}))
	exists, err := service.PathExists(context.Background(), traversal.PathsOptions{SourceID: "source", DestinationID: "destination", MaxPaths: intPtr(99)})
	if err != nil || !exists {
		t.Fatalf("PathExists() = %t, %v", exists, err)
	}
}

func TestReachableUsesVariablesAndDecodesPaths(t *testing.T) {
	t.Parallel()
	service := traversal.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		data := request.Variables["data"].(map[string]any)
		if !reflect.DeepEqual(data["target_kinds"], []string{"Site"}) || data["max_results"] != 10 {
			t.Fatalf("data = %#v", data)
		}
		return decode(dst, `{"InfrahubReachableNodes":{"source":{"id":"source","kind":"Device","label":"Device","display_label":"edge","hfid":["edge"]},"dependencies":[{"node":{"id":"site","kind":"Site","label":"Site","display_label":"zrh","hfid":["zrh"]},"depth":1,"path":{"depth":1,"hops":[]}}],"count":1}}`)
	}))
	result, err := service.Reachable(context.Background(), traversal.ReachableOptions{SourceID: "source", TargetKinds: []string{"Site"}, MaxResults: intPtr(10)})
	if err != nil || result.Count != 1 || result.Dependencies[0].Node.ID != "site" {
		t.Fatalf("Reachable() = %#v, %v", result, err)
	}
}

func TestUnknownFieldReturnsUnsupportedError(t *testing.T) {
	t.Parallel()
	service := traversal.NewService(executorFunc(func(context.Context, api.GraphQLRequest, any) error {
		return &api.GraphQLError{Items: []api.GraphQLErrorItem{{Message: "Cannot query field 'InfrahubPathTraversal' on type 'Query'."}}}
	}))
	_, err := service.Paths(context.Background(), traversal.PathsOptions{SourceID: "source", DestinationID: "destination"})
	var unsupported *traversal.UnsupportedError
	var graphqlError *api.GraphQLError
	if !errors.As(err, &unsupported) || !errors.As(err, &graphqlError) || unsupported.MinimumVersion != "1.10" {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestBusinessGraphQLErrorIsPreserved(t *testing.T) {
	t.Parallel()
	expected := &api.GraphQLError{Items: []api.GraphQLErrorItem{{Message: "Source node not found"}}}
	service := traversal.NewService(executorFunc(func(context.Context, api.GraphQLRequest, any) error { return expected }))
	_, err := service.Paths(context.Background(), traversal.PathsOptions{SourceID: "source", DestinationID: "destination"})
	var unsupported *traversal.UnsupportedError
	if !errors.Is(err, expected) || errors.As(err, &unsupported) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	service := traversal.NewService(nil)
	checks := []func() error{
		func() error { _, err := service.Paths(context.Background(), traversal.PathsOptions{}); return err },
		func() error {
			_, err := service.Paths(context.Background(), traversal.PathsOptions{SourceID: "a", DestinationID: "b", MaxDepth: intPtr(31)})
			return err
		},
		func() error {
			_, err := service.Reachable(context.Background(), traversal.ReachableOptions{SourceID: "a"})
			return err
		},
		func() error {
			_, err := service.Reachable(context.Background(), traversal.ReachableOptions{SourceID: "a", TargetKinds: []string{"Site"}, MaxPaths: intPtr(5001)})
			return err
		},
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Fatal("validation error = nil")
		}
	}
}
