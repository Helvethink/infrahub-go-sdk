package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/task"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}

func decode(dst any, payload string) error { return json.Unmarshal([]byte(payload), dst) }

const taskPayload = `{"InfrahubTask":{"count":1,"edges":[{"node":{"id":"task-1","title":"Import","state":"RUNNING","progress":0.5,"workflow":"import","branch":"main","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z","parameters":{"site":"zrh"},"tags":["sdk"],"related_nodes":[{"id":"node-1","kind":"BuiltinTag"}],"logs":{"edges":[{"node":{"message":"started","severity":"INFO","timestamp":"2026-01-02T03:04:06Z"}}]}}}]}}`

func TestListUsesVariablesAndDecodesOptionalFields(t *testing.T) {
	t.Parallel()
	service := task.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		checks := []string{"$state: [StateType!]", "state: $state", "related_nodes { id kind }", "logs { edges"}
		for _, expected := range checks {
			if !strings.Contains(request.Query, expected) {
				t.Errorf("query missing %q: %s", expected, request.Query)
			}
		}
		if strings.Contains(request.Query, "feature/a") {
			t.Fatalf("filter value interpolated in query: %s", request.Query)
		}
		if request.Variables["branch"] != "feature/a" || request.Variables["offset"] != 5 || request.Variables["limit"] != 10 {
			t.Fatalf("variables = %#v", request.Variables)
		}
		return decode(dst, taskPayload)
	}))
	page, err := service.List(context.Background(), task.ListOptions{
		Filter: task.Filter{Branch: "feature/a", States: []task.State{task.StateRunning}},
		Offset: 5, Limit: 10, IncludeLogs: true, IncludeRelatedNodes: true,
	})
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("List() = %#v, %v", page, err)
	}
	item := page.Tasks[0]
	if item.ID != "task-1" || item.Progress == nil || *item.Progress != 0.5 || len(item.Logs) != 1 || len(item.RelatedNodes) != 1 {
		t.Fatalf("task = %#v", item)
	}
}

func TestAllFollowsPaginationFromOffset(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var offsets []int
	service := task.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		offset := request.Variables["offset"].(int)
		mu.Lock()
		offsets = append(offsets, offset)
		mu.Unlock()
		id := "task-" + string(rune('0'+offset))
		payload := `{"InfrahubTask":{"count":5,"edges":[{"node":{"id":"` + id + `","title":"Task","state":"COMPLETED","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}}]}}`
		return decode(dst, payload)
	}))
	items, err := service.All(context.Background(), task.ListOptions{Offset: 2, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || !reflect.DeepEqual(offsets, []int{2, 3, 4}) {
		t.Fatalf("All() count = %d, offsets = %#v", len(items), offsets)
	}
}

func TestCountUsesCountOnlyQuery(t *testing.T) {
	t.Parallel()
	service := task.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName != "TaskCount" || strings.Contains(request.Query, "edges") {
			t.Fatalf("request = %#v", request)
		}
		return decode(dst, `{"InfrahubTask":{"count":7}}`)
	}))
	count, err := service.Count(context.Background(), task.Filter{Workflows: []string{"import"}})
	if err != nil || count != 7 {
		t.Fatalf("Count() = %d, %v", count, err)
	}
}

func TestGetReturnsTypedLookupErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		check   func(error) bool
	}{
		{name: "missing", payload: `{"InfrahubTask":{"count":0,"edges":[]}}`, check: func(err error) bool {
			var target *api.NotFoundError
			return errors.As(err, &target)
		}},
		{name: "ambiguous", payload: `{"InfrahubTask":{"count":2,"edges":[{"node":{"id":"one","title":"One","state":"COMPLETED","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}},{"node":{"id":"two","title":"Two","state":"COMPLETED","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}}]}}`, check: func(err error) bool {
			var target *task.AmbiguousError
			return errors.As(err, &target)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := task.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
				return decode(dst, test.payload)
			}))
			_, err := service.Get(context.Background(), "task-id", false, false)
			if !test.check(err) {
				t.Fatalf("error = %T %v", err, err)
			}
		})
	}
}

func TestWaitReturnsFinalTaskAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	t.Run("final", func(t *testing.T) {
		var calls int
		service := task.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
			calls++
			state := "RUNNING"
			if calls == 2 {
				state = "FAILED"
			}
			return decode(dst, `{"InfrahubTask":{"count":1,"edges":[{"node":{"id":"task","title":"Task","state":"`+state+`","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}}]}}`)
		}))
		result, err := service.Wait(context.Background(), "task", time.Nanosecond)
		if err != nil || result.State != task.StateFailed || calls != 2 {
			t.Fatalf("Wait() = %#v, %v, calls=%d", result, err, calls)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		service := task.NewService(executorFunc(func(_ context.Context, _ api.GraphQLRequest, dst any) error {
			return decode(dst, `{"InfrahubTask":{"count":1,"edges":[{"node":{"id":"task","title":"Task","state":"RUNNING","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}}]}}`)
		}))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := service.Wait(ctx, "task", time.Hour)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %T %v", err, err)
		}
	})
}

func TestValidation(t *testing.T) {
	t.Parallel()
	service := task.NewService(nil)
	if _, err := service.List(context.Background(), task.ListOptions{Offset: -1}); err == nil {
		t.Fatal("negative offset error = nil")
	}
	if _, err := service.Get(context.Background(), "", false, false); err == nil {
		t.Fatal("empty ID error = nil")
	}
	if _, err := service.Wait(context.Background(), "id", 0); err == nil {
		t.Fatal("zero interval error = nil")
	}
}

func TestExecutionErrorsAndAmbiguousMessage(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("GraphQL failed")
	service := task.NewService(executorFunc(func(context.Context, api.GraphQLRequest, any) error { return sentinel }))
	if _, err := service.List(context.Background(), task.ListOptions{}); !errors.Is(err, sentinel) {
		t.Fatalf("List() error = %v", err)
	}
	if _, err := service.All(context.Background(), task.ListOptions{}); !errors.Is(err, sentinel) {
		t.Fatalf("All() error = %v", err)
	}
	if _, err := service.Get(context.Background(), "task-id", false, false); !errors.Is(err, sentinel) {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := service.Wait(context.Background(), "task-id", time.Second); !errors.Is(err, sentinel) {
		t.Fatalf("Wait() error = %v", err)
	}
	message := (&task.AmbiguousError{ID: "task-id", Count: 2}).Error()
	if !strings.Contains(message, "task-id") || !strings.Contains(message, "2") {
		t.Fatalf("Error() = %q", message)
	}
}

func TestAllStopsOnEmptyPage(t *testing.T) {
	t.Parallel()
	service := task.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.Variables["offset"] == 5 {
			return decode(dst, `{"InfrahubTask":{"count":10,"edges":[]}}`)
		}
		return decode(dst, `{"InfrahubTask":{"count":10,"edges":[{"node":{"id":"one","title":"One","state":"COMPLETED","created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:05:05Z"}}]}}`)
	}))
	items, err := service.All(context.Background(), task.ListOptions{Limit: 5})
	if err != nil || len(items) != 1 {
		t.Fatalf("All() = %#v, %v", items, err)
	}
}
