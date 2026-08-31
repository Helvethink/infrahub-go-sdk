// Package repository provides operations for Git repositories managed by Infrahub.
package repository

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const (
	defaultKind        = "CoreGenericRepository"
	defaultConcurrency = 4
	pageSize           = 1000
)

var kindPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service manages repositories tracked by Infrahub.
type Service struct{ client Executor }

// NewService creates a repository service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// BranchState describes a repository on one Infrahub branch.
type BranchState struct {
	// Commit contains the commit value.
	Commit string
	// InternalStatus contains the internal status value.
	InternalStatus string
}

// Repository is a Git repository aggregated across Infrahub branches.
type Repository struct {
	// ID is the stable Infrahub identifier.
	ID string
	// Kind is the Infrahub schema kind.
	Kind string
	// Name is the human-readable name.
	Name string
	// Location contains the location value.
	Location string
	// Ref contains the ref value.
	Ref string
	// Branches contains the branches value.
	Branches map[string]BranchState
}

// StagingBranch returns the lexicographically first branch marked as staging.
func (r Repository) StagingBranch() (string, bool) {
	branches := make([]string, 0, len(r.Branches))
	for branch, state := range r.Branches {
		if state.InternalStatus == "staging" {
			branches = append(branches, branch)
		}
	}
	sort.Strings(branches)
	if len(branches) == 0 {
		return "", false
	}
	return branches[0], true
}

// ListOptions configures repository discovery across Infrahub branches.
type ListOptions struct {
	// Branches limits discovery to these branches. When empty, List discovers all branches.
	Branches []string
	// Kind selects a repository interface or kind. The default is CoreGenericRepository.
	Kind string
	// Concurrency bounds simultaneous branch queries. Values below one use the default.
	Concurrency int
}

// UpdateCommitOptions configures a repository commit update.
type UpdateCommitOptions struct {
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// RepositoryID contains the repository ID value.
	RepositoryID string
	// Commit contains the commit value.
	Commit string
	// ReadOnly contains the read only value.
	ReadOnly bool
}

// attribute holds internal data used by the attribute workflow.
type attribute struct {
	Value string `json:"value"`
}

// branchRepository holds internal data used by the branch repository workflow.
type branchRepository struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	Name           attribute `json:"name"`
	Location       attribute `json:"location"`
	Commit         attribute `json:"commit"`
	Ref            attribute `json:"ref"`
	InternalStatus attribute `json:"internal_status"`
}

// List returns repositories aggregated by name across the selected branches.
// Results are sorted by repository name and no partial result is returned on error.
func (s *Service) List(ctx context.Context, options ListOptions) ([]Repository, error) {
	kind := options.Kind
	if kind == "" {
		kind = defaultKind
	}
	if !kindPattern.MatchString(kind) {
		return nil, fmt.Errorf("infrahub: invalid repository kind %q", kind)
	}
	branches := append([]string(nil), options.Branches...)
	if len(branches) == 0 {
		var data struct {
			Branch []struct {
				Name string `json:"name"`
			} `json:"Branch"`
		}
		if err := s.client.Execute(ctx, api.GraphQLRequest{
			Query: `query RepositoryBranches { Branch { name } }`, OperationName: "RepositoryBranches",
		}, &data); err != nil {
			return nil, err
		}
		for _, branch := range data.Branch {
			branches = append(branches, branch.Name)
		}
	}
	branches = uniqueSorted(branches)
	if len(branches) == 0 {
		return []Repository{}, nil
	}

	concurrency := options.Concurrency
	if concurrency < 1 {
		concurrency = defaultConcurrency
	}
	if concurrency > len(branches) {
		concurrency = len(branches)
	}
	byBranch, err := s.listBranches(ctx, kind, branches, concurrency)
	if err != nil {
		return nil, err
	}
	return aggregate(branches, byBranch), nil
}

// listBranches discovers the branches used for repository aggregation.
func (s *Service) listBranches(ctx context.Context, kind string, branches []string, concurrency int) (map[string][]branchRepository, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tasks := make(chan string)
	results := make(chan branchResult, len(branches))
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for branch := range tasks {
				repositories, err := s.listBranch(ctx, kind, branch)
				results <- branchResult{branch: branch, repositories: repositories, err: err}
				if err != nil {
					cancel()
					return
				}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for _, branch := range branches {
			select {
			case tasks <- branch:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	byBranch := make(map[string][]branchRepository, len(branches))
	var firstErr error
	for result := range results {
		if result.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("infrahub: list repositories on branch %q: %w", result.branch, result.err)
		}
		if result.err == nil {
			byBranch[result.branch] = result.repositories
		}
	}
	return byBranch, firstErr
}

// branchResult holds internal data used by the branch result workflow.
type branchResult struct {
	branch       string
	repositories []branchRepository
	err          error
}

// listBranch retrieves repository state for one branch.
func (s *Service) listBranch(ctx context.Context, kind, branch string) ([]branchRepository, error) {
	operation := "List" + kind
	fields := `id kind: __typename name { value } location { value } internal_status { value }`
	switch kind {
	case "CoreGenericRepository":
		fields += ` ... on CoreRepository { commit { value } } ... on CoreReadOnlyRepository { commit { value } ref { value } }`
	case "CoreRepository":
		fields += ` commit { value }`
	case "CoreReadOnlyRepository":
		fields += ` commit { value } ref { value }`
	}
	all := make([]branchRepository, 0)
	for offset := 0; ; offset += pageSize {
		var response map[string]struct {
			Count int `json:"count"`
			Edges []struct {
				Node branchRepository `json:"node"`
			} `json:"edges"`
		}
		err := s.client.Execute(ctx, api.GraphQLRequest{
			Query:     `query ` + operation + `($offset: Int!, $limit: Int!) { ` + kind + `(offset: $offset, limit: $limit) { count edges { node { ` + fields + ` } } } }`,
			Variables: map[string]any{"offset": offset, "limit": pageSize}, OperationName: operation, Branch: branch,
		}, &response)
		page := response[kind]
		if err != nil {
			return nil, err
		}
		for _, edge := range page.Edges {
			all = append(all, edge.Node)
		}
		if len(all) >= page.Count || len(page.Edges) == 0 {
			return all, nil
		}
	}
}

// aggregate merges per-branch repository rows by stable repository identity.
func aggregate(branches []string, byBranch map[string][]branchRepository) []Repository {
	byName := make(map[string]*Repository)
	for _, branch := range branches {
		repositories := append([]branchRepository(nil), byBranch[branch]...)
		sort.Slice(repositories, func(i, j int) bool { return repositories[i].Name.Value < repositories[j].Name.Value })
		for _, item := range repositories {
			repository := byName[item.Name.Value]
			if repository == nil {
				repository = &Repository{
					ID: item.ID, Kind: item.Kind, Name: item.Name.Value, Location: item.Location.Value,
					Ref: item.Ref.Value, Branches: make(map[string]BranchState),
				}
				byName[item.Name.Value] = repository
			}
			repository.Branches[branch] = BranchState{Commit: item.Commit.Value, InternalStatus: item.InternalStatus.Value}
		}
	}
	result := make([]Repository, 0, len(byName))
	for _, repository := range byName {
		result = append(result, *repository)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// uniqueSorted returns sorted values with duplicates removed.
func uniqueSorted(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if value == "" || len(result) > 0 && result[len(result)-1] == value {
			continue
		}
		result = append(result, value)
	}
	return result
}

// UpdateCommit updates the protected commit attribute and returns the value
// confirmed by Infrahub. Set ReadOnly for CoreReadOnlyRepository objects.
func (s *Service) UpdateCommit(ctx context.Context, options UpdateCommitOptions) (string, error) {
	if options.RepositoryID == "" {
		return "", fmt.Errorf("infrahub: repository ID must not be empty")
	}
	if options.Commit == "" {
		return "", fmt.Errorf("infrahub: repository commit must not be empty")
	}
	operation := "CoreRepositoryUpdate"
	if options.ReadOnly {
		operation = "CoreReadOnlyRepositoryUpdate"
	}
	var response map[string]struct {
		OK     bool `json:"ok"`
		Object *struct {
			Commit attribute `json:"commit"`
		} `json:"object"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:         `mutation ` + operation + `($repositoryID: String!, $commit: String!) { ` + operation + `(data: { id: $repositoryID, commit: { is_protected: true, source: $repositoryID, value: $commit } }) { ok object { commit { value } } } }`,
		Variables:     map[string]any{"repositoryID": options.RepositoryID, "commit": options.Commit},
		OperationName: operation, Branch: options.Branch, Tracker: "mutation-repository-update-commit",
	}, &response)
	result := response[operation]
	if err != nil {
		if result.Object != nil {
			return result.Object.Commit.Value, err
		}
		return "", err
	}
	if !result.OK || result.Object == nil {
		return "", &api.OperationError{Operation: operation}
	}
	return result.Object.Commit.Value, nil
}
