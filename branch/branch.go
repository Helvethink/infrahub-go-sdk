// Package branch provides operations for Infrahub branches.
package branch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/Helvethink/infrahub-go-sdk/api"
)

// Status describes the lifecycle state of an Infrahub branch.
type Status string

const (
	StatusOpen              Status = "OPEN"
	StatusNeedRebase        Status = "NEED_REBASE"
	StatusNeedUpgradeRebase Status = "NEED_UPGRADE_REBASE"
	StatusDeleting          Status = "DELETING"
	StatusMerging           Status = "MERGING"
	StatusMerged            Status = "MERGED"
)

const fields = `id name description origin_branch branched_from is_default sync_with_git has_schema_changes graph_version status`

// Branch is an Infrahub branch.
type Branch struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Description      *string `json:"description"`
	OriginBranch     *string `json:"origin_branch"`
	BranchedFrom     string  `json:"branched_from"`
	IsDefault        bool    `json:"is_default"`
	SyncWithGit      bool    `json:"sync_with_git"`
	HasSchemaChanges bool    `json:"has_schema_changes"`
	GraphVersion     *int    `json:"graph_version"`
	Status           Status  `json:"status"`
}

// Client is the minimal protocol required by Service.
type Client interface {
	Execute(context.Context, api.GraphQLRequest, any) error
	Endpoint(string, url.Values) *url.URL
	Do(context.Context, string, *url.URL, io.Reader, http.Header, string) ([]byte, error)
}

// Service manages Infrahub branches.
type Service struct{ client Client }

// NewService creates a branch service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// CreateOptions configures branch creation.
type CreateOptions struct {
	Description string
	SyncWithGit bool
}

// List returns all branches.
func (s *Service) List(ctx context.Context) ([]Branch, error) {
	var data struct {
		Branch []Branch `json:"Branch"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query: `query BranchList { Branch { ` + fields + ` } }`, OperationName: "BranchList",
	}, &data)
	return data.Branch, err
}

// Get returns a branch by name.
func (s *Service) Get(ctx context.Context, name string) (*Branch, error) {
	var data struct {
		Branch []Branch `json:"Branch"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `query BranchGet($name: String!) { Branch(name: $name) { ` + fields + ` } }`,
		Variables: map[string]any{"name": name}, OperationName: "BranchGet",
	}, &data)
	if err != nil {
		return nil, err
	}
	if len(data.Branch) == 0 {
		return nil, &api.NotFoundError{Kind: "branch", Identifier: name}
	}
	return &data.Branch[0], nil
}

// Create creates a branch and waits for its resulting branch object.
func (s *Service) Create(ctx context.Context, name string, options CreateOptions) (*Branch, error) {
	var data struct {
		Result struct {
			OK     bool    `json:"ok"`
			Object *Branch `json:"object"`
		} `json:"BranchCreate"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:         `mutation BranchCreate($data: BranchCreateInput!) { BranchCreate(data: $data, wait_until_completion: true) { ok object { ` + fields + ` } } }`,
		Variables:     map[string]any{"data": map[string]any{"name": name, "description": options.Description, "sync_with_git": options.SyncWithGit}},
		OperationName: "BranchCreate", Tracker: "mutation-branch-create",
	}, &data)
	if err != nil {
		return data.Result.Object, err
	}
	if !data.Result.OK || data.Result.Object == nil {
		return nil, &api.OperationError{Operation: "BranchCreate"}
	}
	return data.Result.Object, nil
}

// Delete deletes a branch.
func (s *Service) Delete(ctx context.Context, name string) error {
	return s.simpleMutation(ctx, "BranchDelete", name)
}

// Rebase rebases a branch onto the default branch.
func (s *Service) Rebase(ctx context.Context, name string) error {
	return s.simpleMutation(ctx, "BranchRebase", name)
}

// Validate validates a branch.
func (s *Service) Validate(ctx context.Context, name string) error {
	return s.simpleMutation(ctx, "BranchValidate", name)
}

// Merge merges a branch into the default branch.
func (s *Service) Merge(ctx context.Context, name string) error {
	return s.simpleMutation(ctx, "BranchMerge", name)
}

func (s *Service) simpleMutation(ctx context.Context, operation, name string) error {
	var data map[string]struct {
		OK bool `json:"ok"`
	}
	inputType := "BranchNameInput"
	if operation == "BranchDelete" {
		inputType = "BranchDeleteInput"
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: ` + inputType + `!) { ` + operation + `(data: $data, wait_until_completion: true) { ok } }`,
		Variables: map[string]any{"data": map[string]any{"name": name}}, OperationName: operation,
		Tracker: "mutation-branch-" + operation,
	}, &data)
	if err != nil {
		return err
	}
	if !data[operation].OK {
		return &api.OperationError{Operation: operation}
	}
	return nil
}

// DiffData returns the raw branch diff data REST payload.
func (s *Service) DiffData(ctx context.Context, branch string, branchOnly bool, from, to string, dst any) error {
	query := url.Values{"branch": {branch}, "branch_only": {fmt.Sprintf("%t", branchOnly)}}
	if from != "" {
		query.Set("time_from", from)
	}
	if to != "" {
		query.Set("time_to", to)
	}
	body, err := s.client.Do(ctx, http.MethodGet, s.client.Endpoint("api/diff/data", query), nil, nil, "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("infrahub: decode diff data: %w", err)
	}
	return nil
}
