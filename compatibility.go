package infrahub

import (
	"context"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/automation"
	"github.com/Helvethink/infrahub-go-sdk/pkg/branch"
	diffservice "github.com/Helvethink/infrahub-go-sdk/pkg/diff"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	"github.com/Helvethink/infrahub-go-sdk/pkg/objectstore"
	"github.com/Helvethink/infrahub-go-sdk/pkg/repository"
	"github.com/Helvethink/infrahub-go-sdk/pkg/resourcepool"
	"github.com/Helvethink/infrahub-go-sdk/pkg/schema"
	"github.com/Helvethink/infrahub-go-sdk/pkg/task"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
	"github.com/Helvethink/infrahub-go-sdk/pkg/traversal"
)

// WithTracker returns a child context carrying a request tracker override.
func WithTracker(ctx context.Context, tracker string) context.Context {
	return tracking.WithTracker(ctx, tracker)
}

// NewTrackingGroup creates a request-scoped group collector.
func NewTrackingGroup(options TrackingGroupOptions) (*TrackingGroup, error) {
	return tracking.NewGroup(options)
}

type (
	// GraphQLRequest describes an arbitrary GraphQL operation.
	GraphQLRequest = api.GraphQLRequest
	// HTTPError reports a non-successful HTTP response.
	HTTPError = api.HTTPError
	// GraphQLError reports one or more errors returned in a GraphQL response.
	GraphQLError = api.GraphQLError
	// GraphQLErrorItem describes one error in a GraphQL response.
	GraphQLErrorItem = api.GraphQLErrorItem
	// GraphQLErrorLocation identifies a source location associated with a GraphQL error.
	GraphQLErrorLocation = api.GraphQLErrorLocation
	// OperationError reports an Infrahub operation that did not succeed.
	OperationError = api.OperationError
	// NotFoundError reports that an Infrahub object could not be found.
	NotFoundError = api.NotFoundError

	// AutomationQueryOptions configures execution of an Infrahub named query.
	AutomationQueryOptions = automation.QueryOptions
	// AutomationRunOptions configures a Go-native automation run.
	AutomationRunOptions = automation.RunOptions
	// AutomationTransform transforms the result of a named query.
	AutomationTransform = automation.Transform
	// AutomationGenerator generates side effects from the result of a named query.
	AutomationGenerator = automation.Generator
	// AutomationCheck inspects named-query data and reports findings.
	AutomationCheck = automation.Check
	// AutomationSeverity classifies an automation finding.
	AutomationSeverity = automation.Severity
	// AutomationFinding describes one issue reported by an automation check.
	AutomationFinding = automation.Finding
	// AutomationReporter collects findings during an automation check.
	AutomationReporter = automation.Reporter
	// AutomationCheckResult contains the outcome of an automation check.
	AutomationCheckResult = automation.CheckResult
	// AutomationService executes named queries and Go-native automation callbacks.
	AutomationService = automation.Service

	// Branch describes an Infrahub branch.
	Branch = branch.Branch
	// BranchStatus describes the lifecycle state of an Infrahub branch.
	BranchStatus = branch.Status
	// BranchCreateOptions configures branch creation.
	BranchCreateOptions = branch.CreateOptions
	// BranchService provides branch lifecycle operations.
	BranchService = branch.Service

	// DiffAction describes the action represented by a diff element.
	DiffAction = diffservice.Action
	// DiffElementType identifies the kind of data represented by a diff element.
	DiffElementType = diffservice.ElementType
	// DiffCounts contains aggregate branch diff counts.
	DiffCounts = diffservice.Counts
	// DiffPeer describes one peer in a relationship diff.
	DiffPeer = diffservice.Peer
	// DiffElement describes one changed attribute or relationship.
	DiffElement = diffservice.Element
	// DiffNode describes one changed node.
	DiffNode = diffservice.Node
	// DiffTree contains a complete branch diff tree.
	DiffTree = diffservice.Tree
	// DiffOptions configures branch diff operations.
	DiffOptions = diffservice.Options
	// DiffService provides branch diff operations.
	DiffService = diffservice.Service

	// Node describes a schema-defined Infrahub object.
	Node = node.Node
	// NodePage contains one page of schema-defined nodes.
	NodePage = node.Page
	// NodeMutationResult describes the result of a node mutation.
	NodeMutationResult = node.MutationResult
	// NodeService provides generic schema-defined node operations.
	NodeService = node.Service

	// ObjectStoreService provides stored-object and text-file operations.
	ObjectStoreService = objectstore.Service
	// ObjectStoreUploadResult contains the identifier assigned to uploaded content.
	ObjectStoreUploadResult = objectstore.UploadResult
	// ObjectStoreUnsupportedContentTypeError reports an unsupported file response type.
	ObjectStoreUnsupportedContentTypeError = objectstore.UnsupportedContentTypeError

	// Repository describes an Infrahub repository and its per-branch state.
	Repository = repository.Repository
	// RepositoryBranchState describes repository state on one branch.
	RepositoryBranchState = repository.BranchState
	// RepositoryListOptions configures repository discovery.
	RepositoryListOptions = repository.ListOptions
	// RepositoryUpdateCommitOptions configures a repository commit update.
	RepositoryUpdateCommitOptions = repository.UpdateCommitOptions
	// RepositoryService provides repository operations.
	RepositoryService = repository.Service

	// ResourcePoolAllocation describes an allocated resource.
	ResourcePoolAllocation = resourcepool.Allocation
	// ResourcePoolAddressOptions configures address allocation.
	ResourcePoolAddressOptions = resourcepool.AddressOptions
	// ResourcePoolPrefixOptions configures prefix allocation.
	ResourcePoolPrefixOptions = resourcepool.PrefixOptions
	// ResourcePoolAllocatedOptions configures allocated-resource lookup.
	ResourcePoolAllocatedOptions = resourcepool.AllocatedOptions
	// ResourcePoolAllocationPage contains one page of allocated resources.
	ResourcePoolAllocationPage = resourcepool.AllocationPage
	// ResourcePoolUtilization describes utilization for one resource.
	ResourcePoolUtilization = resourcepool.Utilization
	// ResourcePoolUtilizationResult contains aggregate pool utilization.
	ResourcePoolUtilizationResult = resourcepool.UtilizationResult
	// ResourcePoolMemberType identifies the kind of member allocated from a prefix pool.
	ResourcePoolMemberType = resourcepool.MemberType
	// ResourcePoolService provides resource-pool operations.
	ResourcePoolService = resourcepool.Service

	// TrackingGroup collects nodes observed during a request workflow.
	TrackingGroup = tracking.Group
	// TrackingGroupOptions configures a tracking group.
	TrackingGroupOptions = tracking.GroupOptions
	// TrackingGroupResult describes a saved tracking group.
	TrackingGroupResult = tracking.GroupResult

	// TraversalNode describes a node encountered during graph traversal.
	TraversalNode = traversal.Node
	// TraversalRelationship describes an edge traversed between two nodes.
	TraversalRelationship = traversal.Relationship
	// TraversalHop describes one node and the edge used to reach it.
	TraversalHop = traversal.Hop
	// TraversalPath describes one ordered route through the graph.
	TraversalPath = traversal.Path
	// TraversalPathsResult contains paths between two nodes.
	TraversalPathsResult = traversal.PathsResult
	// TraversalPathsOptions configures path traversal.
	TraversalPathsOptions = traversal.PathsOptions
	// TraversalReachableNode describes a reachable node and its path.
	TraversalReachableNode = traversal.ReachableNode
	// TraversalReachableResult contains nodes reachable from a source.
	TraversalReachableResult = traversal.ReachableResult
	// TraversalReachableOptions configures a reachable-node search.
	TraversalReachableOptions = traversal.ReachableOptions
	// TraversalUnsupportedError reports an unsupported graph traversal operation.
	TraversalUnsupportedError = traversal.UnsupportedError
	// TraversalService provides graph traversal operations.
	TraversalService = traversal.Service

	// Task describes an Infrahub background task.
	Task = task.Task
	// TaskState describes the lifecycle state of a background task.
	TaskState = task.State
	// TaskLog describes one background-task log entry.
	TaskLog = task.Log
	// TaskRelatedNode identifies a node associated with a background task.
	TaskRelatedNode = task.RelatedNode
	// TaskFilter selects background tasks.
	TaskFilter = task.Filter
	// TaskListOptions configures paginated task lookup.
	TaskListOptions = task.ListOptions
	// TaskPage contains one page of background tasks.
	TaskPage = task.Page
	// TaskService provides background-task operations.
	TaskService = task.Service
	// TaskAmbiguousError reports a task lookup that matched multiple tasks.
	TaskAmbiguousError = task.AmbiguousError

	// SchemaService provides schema discovery, validation, and loading operations.
	SchemaService = schema.Service
)

const (
	// AutomationSeverityInfo identifies an informational finding.
	AutomationSeverityInfo = automation.SeverityInfo
	// AutomationSeverityWarning identifies a warning finding.
	AutomationSeverityWarning = automation.SeverityWarning
	// AutomationSeverityError identifies an error finding.
	AutomationSeverityError = automation.SeverityError
)

const (
	// DiffActionAdded identifies an added object or field.
	DiffActionAdded = diffservice.ActionAdded
	// DiffActionUpdated identifies an updated object or field.
	DiffActionUpdated = diffservice.ActionUpdated
	// DiffActionRemoved identifies a removed object or field.
	DiffActionRemoved = diffservice.ActionRemoved
	// DiffActionUnchanged identifies an unchanged object or field.
	DiffActionUnchanged = diffservice.ActionUnchanged
	// DiffActionConflict identifies a conflicting change.
	DiffActionConflict = diffservice.ActionConflict
)

const (
	// DiffElementTypeAttribute identifies an attribute diff.
	DiffElementTypeAttribute = diffservice.ElementTypeAttribute
	// DiffElementTypeRelationshipOne identifies a one-to-one relationship diff.
	DiffElementTypeRelationshipOne = diffservice.ElementTypeRelationshipOne
	// DiffElementTypeRelationshipMany identifies a one-to-many relationship diff.
	DiffElementTypeRelationshipMany = diffservice.ElementTypeRelationshipMany
)

const (
	// ResourcePoolMemberTypePrefix requests allocation of a prefix.
	ResourcePoolMemberTypePrefix = resourcepool.MemberTypePrefix
	// ResourcePoolMemberTypeAddress requests allocation of an address.
	ResourcePoolMemberTypeAddress = resourcepool.MemberTypeAddress
)

const (
	// TaskStateScheduled identifies a task waiting for its scheduled time.
	TaskStateScheduled = task.StateScheduled
	// TaskStatePending identifies a task waiting to start.
	TaskStatePending = task.StatePending
	// TaskStateRunning identifies a task currently executing.
	TaskStateRunning = task.StateRunning
	// TaskStateCompleted identifies a successfully completed task.
	TaskStateCompleted = task.StateCompleted
	// TaskStateFailed identifies a failed task.
	TaskStateFailed = task.StateFailed
	// TaskStateCancelled identifies a cancelled task.
	TaskStateCancelled = task.StateCancelled
	// TaskStateCrashed identifies a task that terminated unexpectedly.
	TaskStateCrashed = task.StateCrashed
	// TaskStatePaused identifies a paused task.
	TaskStatePaused = task.StatePaused
	// TaskStateCancelling identifies a task being cancelled.
	TaskStateCancelling = task.StateCancelling
)

const (
	// BranchStatusOpen identifies a branch available for changes.
	BranchStatusOpen = branch.StatusOpen
	// BranchStatusNeedRebase identifies a branch that must be rebased.
	BranchStatusNeedRebase = branch.StatusNeedRebase
	// BranchStatusNeedUpgradeRebase identifies a branch that requires an upgrade rebase.
	BranchStatusNeedUpgradeRebase = branch.StatusNeedUpgradeRebase
	// BranchStatusDeleting identifies a branch being deleted.
	BranchStatusDeleting = branch.StatusDeleting
	// BranchStatusMerging identifies a branch being merged.
	BranchStatusMerging = branch.StatusMerging
	// BranchStatusMerged identifies a merged branch.
	BranchStatusMerged = branch.StatusMerged
)
