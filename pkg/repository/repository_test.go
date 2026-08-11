package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/repository"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}

func decodeInto(dst any, value string) error {
	return json.Unmarshal([]byte(value), dst)
}

func TestListDiscoversPaginatesAndAggregatesRepositories(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	requests := make([]api.GraphQLRequest, 0)
	service := repository.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		switch request.OperationName {
		case "RepositoryBranches":
			return decodeInto(dst, `{"Branch":[{"name":"main"},{"name":"staging"},{"name":"main"}]}`)
		case "ListCoreGenericRepository":
			branch := request.Branch
			commit, status := branch+"-sha", "active"
			if branch == "staging" {
				status = "staging"
			}
			return decodeInto(dst, `{"CoreGenericRepository":{"count":1,"edges":[{"node":{"id":"repo-id","kind":"CoreRepository","name":{"value":"schemas"},"location":{"value":"https://example.test/repo.git"},"commit":{"value":"`+commit+`"},"ref":{"value":"main"},"internal_status":{"value":"`+status+`"}}}]}}`)
		default:
			return errors.New("unexpected operation: " + request.OperationName)
		}
	}))

	result, err := service.List(context.Background(), repository.ListOptions{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Name != "schemas" || result[0].Branches["main"].Commit != "main-sha" {
		t.Fatalf("List() = %#v", result)
	}
	if branch, ok := result[0].StagingBranch(); !ok || branch != "staging" {
		t.Fatalf("StagingBranch() = %q, %t", branch, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
	for _, request := range requests[1:] {
		if !strings.Contains(request.Query, "$offset: Int!") || request.Variables["limit"] != pageSizeForTest {
			t.Errorf("repository request = %#v", request)
		}
	}
}

const pageSizeForTest = 1000

func TestListUsesExplicitBranchesAndReturnsSortedResults(t *testing.T) {
	t.Parallel()
	service := repository.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName == "RepositoryBranches" {
			t.Fatal("List queried branches despite explicit selection")
		}
		return decodeInto(dst, `{"CoreRepository":{"count":2,"edges":[{"node":{"id":"2","kind":"CoreRepository","name":{"value":"zeta"},"location":{"value":"z"},"commit":{"value":"2"},"ref":{"value":"main"},"internal_status":{"value":"active"}}},{"node":{"id":"1","kind":"CoreRepository","name":{"value":"alpha"},"location":{"value":"a"},"commit":{"value":"1"},"ref":{"value":"main"},"internal_status":{"value":"active"}}}]}}`)
	}))
	result, err := service.List(context.Background(), repository.ListOptions{Branches: []string{"main"}, Kind: "CoreRepository"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].Name != "alpha" || result[1].Name != "zeta" {
		t.Fatalf("List() = %#v", result)
	}
}

func TestListFollowsOffsetPagination(t *testing.T) {
	t.Parallel()
	var offsets []int
	service := repository.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		offset, ok := request.Variables["offset"].(int)
		if !ok {
			t.Fatalf("offset = %T %v", request.Variables["offset"], request.Variables["offset"])
		}
		offsets = append(offsets, offset)
		name := "alpha"
		if offset > 0 {
			name = "beta"
		}
		return decodeInto(dst, `{"CoreGenericRepository":{"count":2,"edges":[{"node":{"id":"id","kind":"CoreRepository","name":{"value":"`+name+`"},"location":{"value":"location"},"commit":{"value":"commit"},"ref":{"value":"main"},"internal_status":{"value":"active"}}}]}}`)
	}))
	result, err := service.List(context.Background(), repository.ListOptions{Branches: []string{"main"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || len(offsets) != 2 || offsets[0] != 0 || offsets[1] != pageSizeForTest {
		t.Fatalf("List() = %#v, offsets = %#v", result, offsets)
	}
}

func TestListRejectsInvalidKind(t *testing.T) {
	t.Parallel()
	service := repository.NewService(nil)
	_, err := service.List(context.Background(), repository.ListOptions{Kind: "CoreRepository) { token"})
	if err == nil {
		t.Fatal("List() error = nil")
	}
}

func TestUpdateCommitUsesVariablesAndReadOnlyMutation(t *testing.T) {
	t.Parallel()
	service := repository.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName != "CoreReadOnlyRepositoryUpdate" || request.Branch != "feature/repo" {
			t.Fatalf("request = %#v", request)
		}
		if strings.Contains(request.Query, "repo-id") || strings.Contains(request.Query, "abc123") {
			t.Fatalf("values were interpolated into query: %s", request.Query)
		}
		if request.Variables["repositoryID"] != "repo-id" || request.Variables["commit"] != "abc123" {
			t.Fatalf("variables = %#v", request.Variables)
		}
		return decodeInto(dst, `{"CoreReadOnlyRepositoryUpdate":{"ok":true,"object":{"commit":{"value":"abc123"}}}}`)
	}))
	commit, err := service.UpdateCommit(context.Background(), repository.UpdateCommitOptions{
		Branch: "feature/repo", RepositoryID: "repo-id", Commit: "abc123", ReadOnly: true,
	})
	if err != nil || commit != "abc123" {
		t.Fatalf("UpdateCommit() = %q, %v", commit, err)
	}
}

func TestUpdateCommitReturnsTypedOperationError(t *testing.T) {
	t.Parallel()
	service := repository.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
		return decodeInto(dst, `{"CoreRepositoryUpdate":{"ok":false,"object":{"commit":{"value":""}}}}`)
	}))
	_, err := service.UpdateCommit(context.Background(), repository.UpdateCommitOptions{RepositoryID: "repo-id", Commit: "abc123"})
	var operationError *api.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestUpdateCommitRejectsMissingObject(t *testing.T) {
	t.Parallel()
	service := repository.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
		return decodeInto(dst, `{"CoreRepositoryUpdate":{"ok":true,"object":null}}`)
	}))
	_, err := service.UpdateCommit(context.Background(), repository.UpdateCommitOptions{RepositoryID: "repo-id", Commit: "abc123"})
	var operationError *api.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestUpdateCommitValidatesRequiredValues(t *testing.T) {
	t.Parallel()
	service := repository.NewService(nil)
	if _, err := service.UpdateCommit(context.Background(), repository.UpdateCommitOptions{Commit: "abc"}); err == nil {
		t.Fatal("empty repository ID error = nil")
	}
	if _, err := service.UpdateCommit(context.Background(), repository.UpdateCommitOptions{RepositoryID: "id"}); err == nil {
		t.Fatal("empty commit error = nil")
	}
}
