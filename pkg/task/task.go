// Package task provides operations for Infrahub background tasks.
package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const defaultPageSize = 50

// State is the lifecycle state of an Infrahub task.
type State string

const (
	// StateScheduled identifies a task waiting for its scheduled start.
	StateScheduled State = "SCHEDULED"
	// StatePending identifies a task waiting to run.
	StatePending State = "PENDING"
	// StateRunning identifies a task currently executing.
	StateRunning State = "RUNNING"
	// StateCompleted identifies a successfully completed task.
	StateCompleted State = "COMPLETED"
	// StateFailed identifies a task that completed with a failure.
	StateFailed State = "FAILED"
	// StateCancelled identifies a cancelled task.
	StateCancelled State = "CANCELLED"
	// StateCrashed identifies a task that terminated unexpectedly.
	StateCrashed State = "CRASHED"
	// StatePaused identifies a paused task.
	StatePaused State = "PAUSED"
	// StateCancelling identifies a task whose cancellation is in progress.
	StateCancelling State = "CANCELLING"
)

// Log is one message emitted by a task.
type Log struct {
	// Message contains the human-readable message.
	Message string `json:"message"`
	// Severity classifies the log entry.
	Severity string `json:"severity"`
	// Timestamp records when the message was emitted.
	Timestamp time.Time `json:"timestamp"`
}

// RelatedNode identifies an Infrahub node related to a task.
type RelatedNode struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Kind is the Infrahub schema kind.
	Kind string `json:"kind"`
}

// Task is an Infrahub background task.
type Task struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Title contains the title value.
	Title string `json:"title"`
	// State contains the state value.
	State State `json:"state"`
	// Progress is the optional completion ratio reported by the task.
	Progress *float64 `json:"progress"`
	// Workflow names the workflow that created the task, when available.
	Workflow *string `json:"workflow"`
	// Branch selects or identifies the Infrahub branch.
	Branch *string `json:"branch"`
	// CreatedAt is the task creation time.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the task's most recent update time.
	UpdatedAt time.Time `json:"updated_at"`
	// Parameters contains the parameters value.
	Parameters map[string]any `json:"parameters"`
	// Tags contains the tags value.
	Tags []string `json:"tags"`
	// RelatedNodes contains the related nodes value.
	RelatedNodes []RelatedNode `json:"related_nodes"`
	// Logs contains the logs value.
	Logs []Log `json:"-"`
}

// IsFinal reports whether the task has reached a terminal state.
func (t Task) IsFinal() bool {
	switch t.State {
	case StateCompleted, StateFailed, StateCancelled, StateCrashed:
		return true
	default:
		return false
	}
}

// Filter selects tasks using stable InfrahubTask query arguments.
type Filter struct {
	// IDs selects tasks by identifier.
	IDs []string
	// Query contains the GraphQL query or query selection.
	Query string
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// States contains the states value.
	States []State
	// Workflows contains the workflows value.
	Workflows []string
	// RelatedNodeIDs selects tasks related to any listed node identifier.
	RelatedNodeIDs []string
}

// ListOptions configures a page of tasks.
type ListOptions struct {
	// Filter contains the filter value.
	Filter Filter
	// Offset is the zero-based pagination offset.
	Offset int
	// Limit is the requested page size.
	Limit int
	// IncludeLogs requests log entries with each task.
	IncludeLogs bool
	// IncludeRelatedNodes requests related-node identities with each task.
	IncludeRelatedNodes bool
}

// Page is one offset-based page of tasks.
type Page struct {
	// Count is the total number of matching items.
	Count int
	// Offset is the zero-based pagination offset.
	Offset int
	// Limit is the requested page size.
	Limit int
	// Tasks contains the tasks in this page.
	Tasks []Task
}

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service manages Infrahub background tasks.
type Service struct{ client Executor }

// NewService creates a task service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// taskNode holds internal data used by the task node workflow.
type taskNode struct {
	Task
	Logs struct {
		Edges []struct {
			Node Log `json:"node"`
		} `json:"edges"`
	} `json:"logs"`
}

// List returns one page of tasks. A zero limit uses 50 tasks per page.
// When GraphQL returns partial data and errors, the page and error are both returned.
func (s *Service) List(ctx context.Context, options ListOptions) (*Page, error) {
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("infrahub: task offset and limit must not be negative")
	}
	limit := options.Limit
	if limit == 0 {
		limit = defaultPageSize
	}
	query, variables := buildQuery(options, true, limit)
	var response struct {
		Tasks struct {
			Count int `json:"count"`
			Edges []struct {
				Node taskNode `json:"node"`
			} `json:"edges"`
		} `json:"InfrahubTask"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query: query, Variables: variables, OperationName: "TaskList", Tracker: "query-tasks-list",
	}, &response)
	page := &Page{Count: response.Tasks.Count, Offset: options.Offset, Limit: limit, Tasks: make([]Task, 0, len(response.Tasks.Edges))}
	for _, edge := range response.Tasks.Edges {
		item := edge.Node.Task
		for _, logEdge := range edge.Node.Logs.Edges {
			item.Logs = append(item.Logs, logEdge.Node)
		}
		page.Tasks = append(page.Tasks, item)
	}
	return page, err
}

// All retrieves every task matching options.Filter. Limit controls the page
// size rather than the total number returned.
func (s *Service) All(ctx context.Context, options ListOptions) ([]Task, error) {
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("infrahub: task offset and limit must not be negative")
	}
	all := make([]Task, 0)
	for {
		page, err := s.List(ctx, options)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Tasks...)
		if options.Offset+len(page.Tasks) >= page.Count || len(page.Tasks) == 0 {
			return all, nil
		}
		options.Offset += page.Limit
	}
}

// Count returns the number of tasks matching filter.
func (s *Service) Count(ctx context.Context, filter Filter) (int, error) {
	options := ListOptions{Filter: filter}
	query, variables := buildQuery(options, false, 0)
	var response struct {
		Tasks struct {
			Count int `json:"count"`
		} `json:"InfrahubTask"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query: query, Variables: variables, OperationName: "TaskCount", Tracker: "query-tasks-count",
	}, &response)
	return response.Tasks.Count, err
}

// Get returns one task by ID.
func (s *Service) Get(ctx context.Context, id string, includeLogs, includeRelatedNodes bool) (*Task, error) {
	if id == "" {
		return nil, fmt.Errorf("infrahub: task ID must not be empty")
	}
	page, err := s.List(ctx, ListOptions{
		Filter: Filter{IDs: []string{id}}, Limit: 2,
		IncludeLogs: includeLogs, IncludeRelatedNodes: includeRelatedNodes,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Tasks) == 0 {
		return nil, &api.NotFoundError{Kind: "task", Identifier: id}
	}
	if len(page.Tasks) > 1 {
		return nil, &AmbiguousError{ID: id, Count: len(page.Tasks)}
	}
	return &page.Tasks[0], nil
}

// AmbiguousError reports multiple tasks returned for one ID.
type AmbiguousError struct {
	// ID is the stable Infrahub identifier.
	ID string
	// Count is the total number of matching items.
	Count int
}

// Error reports that a task lookup matched more than one task.
func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("infrahub: expected one task %q, received %d", e.ID, e.Count)
}

// Wait polls until a task reaches a final state. Cancellation and timeouts are
// controlled by ctx. Interval must be positive.
func (s *Service) Wait(ctx context.Context, id string, interval time.Duration) (*Task, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("infrahub: task polling interval must be positive")
	}
	for {
		task, err := s.Get(ctx, id, false, false)
		if err != nil {
			return nil, err
		}
		if task.IsFinal() {
			return task, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// buildQuery builds the query.
func buildQuery(options ListOptions, includeEdges bool, limit int) (string, map[string]any) {
	definitions := []string{"$ids: [String!]", "$q: String", "$branch: String", "$state: [StateType!]", "$workflow: [String!]", "$relatedNodeIDs: [String!]"}
	arguments := []string{"ids: $ids", "q: $q", "branch: $branch", "state: $state", "workflow: $workflow", "related_node__ids: $relatedNodeIDs"}
	variables := map[string]any{
		"ids": options.Filter.IDs, "q": nullableString(options.Filter.Query), "branch": nullableString(options.Filter.Branch),
		"state": options.Filter.States, "workflow": options.Filter.Workflows, "relatedNodeIDs": options.Filter.RelatedNodeIDs,
	}
	selection := "count"
	if includeEdges {
		definitions = append(definitions, "$offset: Int!", "$limit: Int!")
		arguments = append(arguments, "offset: $offset", "limit: $limit")
		variables["offset"], variables["limit"] = options.Offset, limit
		fields := "id title state progress workflow branch created_at updated_at parameters tags"
		if options.IncludeRelatedNodes {
			fields += " related_nodes { id kind }"
		}
		if options.IncludeLogs {
			fields += " logs { edges { node { message severity timestamp } } }"
		}
		selection += " edges { node { " + fields + " } }"
	}
	operation := "TaskCount"
	if includeEdges {
		operation = "TaskList"
	}
	query := "query " + operation + "(" + strings.Join(definitions, ", ") + ") { InfrahubTask(" + strings.Join(arguments, ", ") + ") { " + selection + " } }"
	return query, variables
}

// nullableString converts an empty string to a GraphQL null value.
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
