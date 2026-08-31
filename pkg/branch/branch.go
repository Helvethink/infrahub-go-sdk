// Package branch provides operations for Infrahub branches.
package branch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

// Status describes the lifecycle state of an Infrahub branch.
type Status string

const (
	// StatusOpen identifies a branch available for changes.
	StatusOpen Status = "OPEN"
	// StatusNeedRebase identifies a branch that must be rebased.
	StatusNeedRebase Status = "NEED_REBASE"
	// StatusNeedUpgradeRebase identifies a branch that requires an upgrade rebase.
	StatusNeedUpgradeRebase Status = "NEED_UPGRADE_REBASE"
	// StatusDeleting identifies a branch being deleted.
	StatusDeleting Status = "DELETING"
	// StatusMerging identifies a branch being merged.
	StatusMerging Status = "MERGING"
	// StatusMerged identifies a branch that has been merged.
	StatusMerged Status = "MERGED"
)

const fields = `id name description origin_branch branched_from is_default sync_with_git has_schema_changes graph_version status`

// Branch is an Infrahub branch.
type Branch struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Name is the human-readable name.
	Name string `json:"name"`
	// Description contains the optional human-readable description.
	Description *string `json:"description"`
	// OriginBranch names the branch from which this branch originated.
	OriginBranch *string `json:"origin_branch"`
	// BranchedFrom contains the source branch revision.
	BranchedFrom string `json:"branched_from"`
	// IsDefault reports whether this is the server's default branch.
	IsDefault bool `json:"is_default"`
	// SyncWithGit reports whether Git synchronization is enabled.
	SyncWithGit bool `json:"sync_with_git"`
	// HasSchemaChanges reports whether the branch changes the schema.
	HasSchemaChanges bool `json:"has_schema_changes"`
	// GraphVersion identifies the current graph revision when available.
	GraphVersion *int `json:"graph_version"`
	// Status is the current lifecycle status.
	Status Status `json:"status"`
}

// Client is the minimal protocol required by Service.
type Client interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service manages Infrahub branches.
type Service struct{ client Client }

// NewService creates a branch service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// CreateOptions configures branch creation.
type CreateOptions struct {
	// Description contains the optional human-readable description.
	Description string
	// SyncWithGit enables Git synchronization for the new branch.
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

// simpleMutation executes a name-based branch mutation and verifies its ok flag.
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

// DiffData returns the raw branch DiffTree GraphQL payload.
// Callers that prefer a typed result can use diff.Service.Tree.
func (s *Service) DiffData(ctx context.Context, branch string, branchOnly bool, from, to string, dst any) error {
	if branch == "" {
		return fmt.Errorf("infrahub: diff branch must not be empty")
	}
	if dst == nil {
		return fmt.Errorf("infrahub: diff destination must not be nil")
	}
	var response struct {
		Tree json.RawMessage `json:"DiffTree"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query: `query BranchDiffData($branch: String!, $fromTime: DateTime, $toTime: DateTime, $includeParents: Boolean!) {
			DiffTree(branch: $branch, from_time: $fromTime, to_time: $toTime, include_parents: $includeParents) {
				name from_time to_time base_branch diff_branch num_added num_updated num_removed num_conflicts
				num_untracked_base_changes num_untracked_diff_changes
				nodes {
					uuid kind status label num_added num_updated num_removed
					attributes { name status num_added num_updated num_removed }
					relationships {
						name status cardinality num_added num_updated num_removed
						elements { status num_added num_updated num_removed }
					}
				}
			}
		}`,
		Variables: map[string]any{
			"branch": branch, "fromTime": nullableDiffValue(from), "toTime": nullableDiffValue(to),
			"includeParents": !branchOnly,
		},
		OperationName: "BranchDiffData", Branch: branch, Tracker: "query-branch-diff-data",
	}, &response)
	if err != nil {
		return err
	}
	if len(response.Tree) == 0 || string(response.Tree) == "null" {
		return nil
	}
	if err := json.Unmarshal(response.Tree, dst); err != nil {
		return fmt.Errorf("infrahub: decode diff data: %w", err)
	}
	return nil
}

// nullableDiffValue converts an empty diff value to GraphQL null.
func nullableDiffValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}
