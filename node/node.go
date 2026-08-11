// Package node provides generic operations for schema-defined Infrahub nodes.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/Helvethink/infrahub-go-sdk/api"
)

var kindPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

const identityFields = `id kind hfid display_label`

// Node identifies an Infrahub object while preserving its dynamic fields.
type Node struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	HFID         []any          `json:"hfid"`
	DisplayLabel string         `json:"display_label"`
	Fields       map[string]any `json:"-"`
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
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service performs generic CRUD operations for dynamic Infrahub kinds.
type Service struct{ client Executor }

// NewService creates a node service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// Page is one offset-based page of Infrahub nodes.
type Page struct {
	Count  int
	Offset int
	Limit  int
	Nodes  []Node
}

// MutationResult is returned by create and update mutations.
type MutationResult struct {
	OK     bool  `json:"ok"`
	Object *Node `json:"object"`
}

// List returns identity fields for a page of nodes. Use Client.Execute when
// custom attributes or relationships must be selected.
func (s *Service) List(ctx context.Context, kind string, offset, limit int, branch string) (*Page, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}
	if offset < 0 || limit < 0 {
		return nil, fmt.Errorf("infrahub: offset and limit must not be negative")
	}
	operation := "List" + kind
	page, err := s.queryPage(ctx, kind, api.GraphQLRequest{
		Query:     `query ` + operation + `($offset: Int, $limit: Int) { ` + kind + `(offset: $offset, limit: $limit) { count edges { node { ` + identityFields + ` } } } }`,
		Variables: map[string]any{"offset": offset, "limit": limit}, OperationName: operation, Branch: branch,
	})
	if page != nil {
		page.Offset, page.Limit = offset, limit
	}
	return page, err
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
	return s.mutate(ctx, kind, "Create", data, branch)
}

// Update updates a node of kind. Data must include an Infrahub identifier.
func (s *Service) Update(ctx context.Context, kind string, data map[string]any, branch string) (*Node, error) {
	return s.mutate(ctx, kind, "Update", data, branch)
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

func (s *Service) mutate(ctx context.Context, kind, action string, input map[string]any, branch string) (*Node, error) {
	if err := validateKind(kind); err != nil {
		return nil, err
	}
	operation := kind + action
	var data map[string]MutationResult
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: ` + operation + `Input!) { ` + operation + `(data: $data) { ok object { ` + identityFields + ` } } }`,
		Variables: map[string]any{"data": input}, OperationName: operation, Branch: branch,
		Tracker: "mutation-node-" + action,
	}, &data)
	result := data[operation]
	if err != nil {
		return result.Object, err
	}
	if !result.OK || result.Object == nil {
		return nil, &api.OperationError{Operation: operation}
	}
	return result.Object, nil
}

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
	return &page.Nodes[0], nil
}

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

func validateKind(kind string) error {
	if !kindPattern.MatchString(kind) {
		return fmt.Errorf("infrahub: invalid kind %q", kind)
	}
	return nil
}
