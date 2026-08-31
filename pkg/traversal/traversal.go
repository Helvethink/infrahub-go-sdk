// Package traversal provides path and reachable-node graph traversal operations.
package traversal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const (
	nodeFields         = `id kind label display_label hfid`
	relationshipFields = `from_rel from_label to_rel to_label kind`
	pathFields         = `hops { node { ` + nodeFields + ` } relationship { ` + relationshipFields + ` } } depth`
)

// Node is the stable identity of a node encountered during traversal.
type Node struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Kind is the Infrahub schema kind.
	Kind string `json:"kind"`
	// Label contains the label value.
	Label string `json:"label"`
	// DisplayLabel is the human-readable display label.
	DisplayLabel string `json:"display_label"`
	// HFID contains the object's human-friendly identifier components.
	HFID []string `json:"hfid"`
}

// Relationship is an edge traversed between two nodes.
type Relationship struct {
	// FromRelationship contains the from relationship value.
	FromRelationship string `json:"from_rel"`
	// FromLabel contains the from label value.
	FromLabel string `json:"from_label"`
	// ToRelationship contains the to relationship value.
	ToRelationship string `json:"to_rel"`
	// ToLabel contains the to label value.
	ToLabel string `json:"to_label"`
	// Kind is the Infrahub schema kind.
	Kind string `json:"kind"`
}

// Hop is one visited node and the edge used to reach it. Relationship is nil for the source hop.
type Hop struct {
	// Node contains the node value.
	Node Node `json:"node"`
	// Relationship contains the relationship value.
	Relationship *Relationship `json:"relationship"`
}

// Path is one ordered route through the graph.
type Path struct {
	// Hops contains the hops value.
	Hops []Hop `json:"hops"`
	// Depth contains the depth value.
	Depth int `json:"depth"`
}

// PathsResult contains routes between a source and destination.
type PathsResult struct {
	// Paths contains the paths value.
	Paths []Path `json:"paths"`
	// Source contains the source value.
	Source Node `json:"source"`
	// Destination contains the destination value.
	Destination Node `json:"destination"`
	// Count is the total number of matching items.
	Count int `json:"count"`
	// ExcludedKinds contains the excluded kinds value.
	ExcludedKinds []string `json:"excluded_kinds"`
	// TruncatedAtDepth contains the truncated at depth value.
	TruncatedAtDepth *int `json:"truncated_at_depth"`
}

// PathsOptions configures traversal between two nodes.
type PathsOptions struct {
	// SourceID identifies the traversal source node.
	SourceID string
	// DestinationID identifies the traversal destination node.
	DestinationID string
	// MaxDepth limits traversal depth.
	MaxDepth *int
	// MaxPaths limits the number of returned paths.
	MaxPaths *int
	// KindFilter contains the kind filter value.
	KindFilter []string
	// RelationshipFilter contains the relationship filter value.
	RelationshipFilter []string
	// ExcludedNamespaces contains the excluded namespaces value.
	ExcludedNamespaces []string
	// ExcludedKinds contains the excluded kinds value.
	ExcludedKinds []string
	// IncludedKinds contains the included kinds value.
	IncludedKinds []string
	// ShortestPathsOnly contains the shortest paths only value.
	ShortestPathsOnly *bool
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// At selects an optional point in time.
	At time.Time
}

// ReachableNode is one reachable terminal and its path from the source.
type ReachableNode struct {
	// Node contains the node value.
	Node Node `json:"node"`
	// Depth contains the depth value.
	Depth int `json:"depth"`
	// Path identifies the GraphQL response path.
	Path Path `json:"path"`
}

// ReachableResult contains nodes of requested kinds reachable from a source.
type ReachableResult struct {
	// Source contains the source value.
	Source Node `json:"source"`
	// Dependencies contains the dependencies value.
	Dependencies []ReachableNode `json:"dependencies"`
	// Count is the total number of matching items.
	Count int `json:"count"`
}

// ReachableOptions configures a reachable-node search.
type ReachableOptions struct {
	// SourceID identifies the traversal source node.
	SourceID string
	// TargetKinds selects the kinds to find.
	TargetKinds []string
	// MaxDepth limits traversal depth.
	MaxDepth *int
	// MaxResults limits the number of returned nodes.
	MaxResults *int
	// MaxPaths limits the number of returned paths.
	MaxPaths *int
	// ShortestPathsOnly contains the shortest paths only value.
	ShortestPathsOnly *bool
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// At selects an optional point in time.
	At time.Time
}

// UnsupportedError reports a server version without graph traversal fields.
type UnsupportedError struct {
	// Operation contains the operation value.
	Operation string
	// MinimumVersion is the earliest Infrahub version supporting the operation.
	MinimumVersion string
	// Err is the underlying error.
	Err error
}

// Error reports the minimum Infrahub version required by the operation.
func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("infrahub: %s requires Infrahub %s or later", e.Operation, e.MinimumVersion)
}

// Unwrap returns the underlying GraphQL validation error.
func (e *UnsupportedError) Unwrap() error { return e.Err }

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service traverses the Infrahub graph.
type Service struct{ client Executor }

// NewService creates a traversal service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// Paths finds paths between two nodes. Partial GraphQL data is returned with its error.
func (s *Service) Paths(ctx context.Context, options PathsOptions) (*PathsResult, error) {
	if options.SourceID == "" || options.DestinationID == "" {
		return nil, fmt.Errorf("infrahub: traversal source and destination IDs must not be empty")
	}
	if err := validateRange("max depth", options.MaxDepth, 30); err != nil {
		return nil, err
	}
	if err := validateRange("max paths", options.MaxPaths, 100); err != nil {
		return nil, err
	}
	input := map[string]any{"source_id": options.SourceID, "destination_id": options.DestinationID}
	setOptional(input, "max_depth", options.MaxDepth)
	setOptional(input, "max_paths", options.MaxPaths)
	setOptional(input, "kind_filter", options.KindFilter)
	setOptional(input, "relationship_filter", options.RelationshipFilter)
	setOptional(input, "excluded_namespaces", options.ExcludedNamespaces)
	setOptional(input, "excluded_kinds", options.ExcludedKinds)
	setOptional(input, "included_kinds", options.IncludedKinds)
	setOptional(input, "shortest_paths_only", options.ShortestPathsOnly)
	var response struct {
		Result PathsResult `json:"InfrahubPathTraversal"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `query InfrahubPathTraversal($data: PathTraversalInput!) { InfrahubPathTraversal(data: $data) { paths { ` + pathFields + ` } source { ` + nodeFields + ` } destination { ` + nodeFields + ` } count excluded_kinds truncated_at_depth } }`,
		Variables: map[string]any{"data": input}, OperationName: "InfrahubPathTraversal", Branch: options.Branch, At: options.At, Tracker: "query-path-traversal",
	}, &response)
	recordPaths(ctx, &response.Result)
	if unsupported(err, "InfrahubPathTraversal") {
		return nil, &UnsupportedError{Operation: "graph path traversal", MinimumVersion: "1.10", Err: err}
	}
	return &response.Result, err
}

// PathExists efficiently checks for at least one path between two nodes.
func (s *Service) PathExists(ctx context.Context, options PathsOptions) (bool, error) {
	one := 1
	options.MaxPaths = &one
	result, err := s.Paths(ctx, options)
	return result != nil && len(result.Paths) > 0, err
}

// Reachable finds nodes of target kinds reachable from a source. Partial GraphQL data is returned with its error.
func (s *Service) Reachable(ctx context.Context, options ReachableOptions) (*ReachableResult, error) {
	if options.SourceID == "" {
		return nil, fmt.Errorf("infrahub: traversal source ID must not be empty")
	}
	if len(options.TargetKinds) == 0 {
		return nil, fmt.Errorf("infrahub: traversal target kinds must not be empty")
	}
	if err := validateRange("max depth", options.MaxDepth, 30); err != nil {
		return nil, err
	}
	if err := validateRange("max results", options.MaxResults, 200); err != nil {
		return nil, err
	}
	if err := validateRange("max paths", options.MaxPaths, 5000); err != nil {
		return nil, err
	}
	input := map[string]any{"source_id": options.SourceID, "target_kinds": append([]string(nil), options.TargetKinds...)}
	setOptional(input, "max_depth", options.MaxDepth)
	setOptional(input, "max_results", options.MaxResults)
	setOptional(input, "max_paths", options.MaxPaths)
	setOptional(input, "shortest_paths_only", options.ShortestPathsOnly)
	var response struct {
		Result ReachableResult `json:"InfrahubReachableNodes"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `query InfrahubReachableNodes($data: ReachableNodesInput!) { InfrahubReachableNodes(data: $data) { source { ` + nodeFields + ` } dependencies { node { ` + nodeFields + ` } depth path { ` + pathFields + ` } } count } }`,
		Variables: map[string]any{"data": input}, OperationName: "InfrahubReachableNodes", Branch: options.Branch, At: options.At, Tracker: "query-reachable-nodes",
	}, &response)
	recordReachable(ctx, &response.Result)
	if unsupported(err, "InfrahubReachableNodes") {
		return nil, &UnsupportedError{Operation: "graph reachable-nodes traversal", MinimumVersion: "1.10", Err: err}
	}
	return &response.Result, err
}

// validateRange validates the range.
func validateRange(name string, value *int, maximum int) error {
	if value != nil && (*value < 1 || *value > maximum) {
		return fmt.Errorf("infrahub: traversal %s must be between 1 and %d", name, maximum)
	}
	return nil
}

// setOptional sets the optional.
func setOptional(input map[string]any, name string, value any) {
	switch typed := value.(type) {
	case *int:
		if typed != nil {
			input[name] = *typed
		}
	case *bool:
		if typed != nil {
			input[name] = *typed
		}
	case []string:
		if typed != nil {
			input[name] = append([]string(nil), typed...)
		}
	}
}

// unsupported recognizes GraphQL validation failures for unavailable traversal fields.
func unsupported(err error, field string) bool {
	var graphqlError *api.GraphQLError
	if !errors.As(err, &graphqlError) {
		return false
	}
	markers := []string{"cannot query field", "unknown field", "doesn't exist", "does not exist"}
	for _, item := range graphqlError.Items {
		message := strings.ToLower(item.Message)
		if !strings.Contains(message, strings.ToLower(field)) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

// recordPaths records the paths.
func recordPaths(ctx context.Context, result *PathsResult) {
	ids := []string{result.Source.ID, result.Destination.ID}
	for _, path := range result.Paths {
		for _, hop := range path.Hops {
			ids = append(ids, hop.Node.ID)
		}
	}
	requestcontext.RecordNodeIDs(ctx, ids...)
}

// recordReachable records the reachable.
func recordReachable(ctx context.Context, result *ReachableResult) {
	ids := []string{result.Source.ID}
	for _, dependency := range result.Dependencies {
		ids = append(ids, dependency.Node.ID)
		for _, hop := range dependency.Path.Hops {
			ids = append(ids, hop.Node.ID)
		}
	}
	requestcontext.RecordNodeIDs(ctx, ids...)
}
