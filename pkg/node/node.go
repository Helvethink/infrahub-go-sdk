// Package node provides generic operations for schema-defined Infrahub nodes.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

var kindPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

const (
	identityFields                    = `id kind: __typename hfid display_label`
	identityFieldsWithoutDisplayLabel = `id kind: __typename hfid`
)

// Node identifies an Infrahub object while preserving its dynamic fields.
type Node struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Kind is the Infrahub schema kind.
	Kind string `json:"kind"`
	// HFID contains the object's human-friendly identifier components.
	HFID []any `json:"hfid"`
	// DisplayLabel is the human-readable display label.
	DisplayLabel string `json:"display_label"`
	// Fields contains dynamically selected schema-defined fields.
	Fields map[string]any `json:"-"`
}

// UnmarshalJSON preserves every selected schema-specific field in Fields.
func (n *Node) UnmarshalJSON(data []byte) error {
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	n.Fields = fields
	if value, ok := fields["id"].(string); ok {
		n.ID = value
	}
	if value, ok := fields["kind"].(string); ok {
		n.Kind = value
	}
	if value, ok := fields["display_label"].(string); ok {
		n.DisplayLabel = value
	}
	if value, ok := fields["hfid"].([]any); ok {
		n.HFID = value
	}
	return nil
}

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service performs generic CRUD operations for dynamic Infrahub kinds.
type Service struct{ client Executor }

// NewService creates a node service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// Page is one offset-based page of Infrahub nodes.
type Page struct {
	// Count is the total number of matching items.
	Count int
	// Offset is the zero-based pagination offset.
	Offset int
	// Limit is the requested page size.
	Limit int
	// Nodes contains the nodes in this result.
	Nodes []Node
}

// MutationResult is returned by create and update mutations.
type MutationResult struct {
	// OK contains the ok value.
	OK bool `json:"ok"`
	// Object contains the object value.
	Object *Node `json:"object"`
}

// MutationOptions configures a dynamic node create, update, or upsert.
type MutationOptions struct {
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// OmitDisplayLabel omits display_label from the mutation response. Infrahub
	// still computes and stores the display label as part of the mutation.
	OmitDisplayLabel bool
}

// List returns identity fields for a page of nodes. Use Query for dynamic
// filters, attributes, or relationships.
func (s *Service) List(ctx context.Context, kind string, offset, limit int, branch string) (*Page, error) {
	return s.Query(ctx, kind, QueryOptions{Offset: offset, Limit: limit, Branch: branch})
}

// GetByID returns a node by its UUID.
func (s *Service) GetByID(ctx context.Context, kind, id, branch string) (*Node, error) {
	if id == "" {
		return nil, fmt.Errorf("infrahub: node ID must not be empty")
	}
	return s.getOne(ctx, kind, "ID", id, `[ID!]`, []string{id}, branch)
}

// GetByHFID returns a node by its human-friendly ID components.
func (s *Service) GetByHFID(ctx context.Context, kind string, hfid []string, branch string) (*Node, error) {
	if len(hfid) == 0 {
		return nil, fmt.Errorf("infrahub: node HFID must not be empty")
	}
	return s.getOne(ctx, kind, "HFID", fmt.Sprint(hfid), `[String!]`, hfid, branch)
}

// Create creates a node of kind using its generated GraphQL input type.
func (s *Service) Create(ctx context.Context, kind string, data map[string]any, branch string) (*Node, error) {
	return s.CreateWithOptions(ctx, kind, data, MutationOptions{Branch: branch})
}

// CreateWithOptions creates a node of kind using its generated GraphQL input
// type and controls the returned identity fields.
func (s *Service) CreateWithOptions(
	ctx context.Context,
	kind string,
	data map[string]any,
	options MutationOptions,
) (*Node, error) {
	return s.mutate(ctx, kind, "Create", data, options)
}

// Update updates a node of kind. Data must include an Infrahub identifier.
func (s *Service) Update(ctx context.Context, kind string, data map[string]any, branch string) (*Node, error) {
	return s.UpdateWithOptions(ctx, kind, data, MutationOptions{Branch: branch})
}

// UpdateWithOptions updates a node of kind and controls the returned identity
// fields. Data must include an Infrahub identifier.
func (s *Service) UpdateWithOptions(
	ctx context.Context,
	kind string,
	data map[string]any,
	options MutationOptions,
) (*Node, error) {
	return s.mutate(ctx, kind, "Update", data, options)
}

// Upsert creates or updates a node of kind using its generated GraphQL input type.
func (s *Service) Upsert(ctx context.Context, kind string, data map[string]any, branch string) (*Node, error) {
	return s.UpsertWithOptions(ctx, kind, data, MutationOptions{Branch: branch})
}

// UpsertWithOptions creates or updates a node of kind and controls the returned
// identity fields.
func (s *Service) UpsertWithOptions(
	ctx context.Context,
	kind string,
	data map[string]any,
	options MutationOptions,
) (*Node, error) {
	return s.mutate(ctx, kind, "Upsert", data, options)
}

// Delete deletes a node of kind. Data usually contains id or hfid.
func (s *Service) Delete(ctx context.Context, kind string, data map[string]any, branch string) error {
	if err := validateKind(kind); err != nil {
		return err
	}
	operation := kind + "Delete"
	var result map[string]struct {
		OK bool `json:"ok"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: DeleteInput!) { ` + operation + `(data: $data) { ok } }`,
		Variables: map[string]any{"data": data}, OperationName: operation, Branch: branch,
		Tracker: "mutation-node-delete",
	}, &result)
	if err != nil {
		return err
	}
	if !result[operation].OK {
		return &api.OperationError{Operation: operation}
	}
	return nil
}

// mutate executes a dynamic node mutation and validates its operation result.
func (s *Service) mutate(
	ctx context.Context,
	kind, action string,
	input map[string]any,
	options MutationOptions,
) (*Node, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}
	operation := kind + action
	fields := identityFields
	if options.OmitDisplayLabel {
		fields = identityFieldsWithoutDisplayLabel
	}
	var data map[string]MutationResult
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: ` + operation + `Input!) { ` + operation + `(data: $data) { ok object { ` + fields + ` } } }`,
		Variables: map[string]any{"data": input}, OperationName: operation, Branch: options.Branch,
		Tracker: "mutation-node-" + action,
	}, &data)
	result := data[operation]
	if result.Object != nil {
		requestcontext.RecordNodeIDs(ctx, result.Object.ID)
	}
	if err != nil {
		return result.Object, err
	}
	if !result.OK || result.Object == nil {
		return nil, &api.OperationError{Operation: operation}
	}
	return result.Object, nil
}

// getOne gets the one.
func (s *Service) getOne(ctx context.Context, kind, suffix, identifier, variableType string, value any, branch string) (*Node, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}
	argument := "ids"
	if suffix == "HFID" {
		argument = "hfid"
	}
	operation := "Get" + kind + "By" + suffix
	page, err := s.queryPage(ctx, kind, api.GraphQLRequest{
		Query:     `query ` + operation + `($value: ` + variableType + `) { ` + kind + `(` + argument + `: $value, limit: 2) { count edges { node { ` + identityFields + ` } } } }`,
		Variables: map[string]any{"value": value}, OperationName: operation, Branch: branch,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Nodes) == 0 {
		return nil, &api.NotFoundError{Kind: kind, Identifier: identifier}
	}
	requestcontext.RecordNodeIDs(ctx, page.Nodes[0].ID)
	return &page.Nodes[0], nil
}

// queryPage executes a generated node query and normalizes its page metadata.
func (s *Service) queryPage(ctx context.Context, kind string, request api.GraphQLRequest) (*Page, error) {
	var response map[string]struct {
		Count int `json:"count"`
		Edges []struct {
			Node Node `json:"node"`
		} `json:"edges"`
	}
	err := s.client.Execute(ctx, request, &response)
	result := response[kind]
	page := &Page{Count: result.Count, Nodes: make([]Node, 0, len(result.Edges))}
	for _, edge := range result.Edges {
		page.Nodes = append(page.Nodes, edge.Node)
	}
	return page, err
}

// validateKind validates the kind.
func validateKind(kind string) error {
	if !kindPattern.MatchString(kind) {
		return fmt.Errorf("infrahub: invalid kind %q", kind)
	}
	return nil
}
