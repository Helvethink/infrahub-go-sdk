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

// Root-package aliases keep common SDK types discoverable and preserve the
// API that predates the domain package split.
type (
	GraphQLRequest       = api.GraphQLRequest
	HTTPError            = api.HTTPError
	GraphQLError         = api.GraphQLError
	GraphQLErrorItem     = api.GraphQLErrorItem
	GraphQLErrorLocation = api.GraphQLErrorLocation
	OperationError       = api.OperationError
	NotFoundError        = api.NotFoundError

	AutomationQueryOptions = automation.QueryOptions
	AutomationRunOptions   = automation.RunOptions
	AutomationTransform    = automation.Transform
	AutomationGenerator    = automation.Generator
	AutomationCheck        = automation.Check
	AutomationSeverity     = automation.Severity
	AutomationFinding      = automation.Finding
	AutomationReporter     = automation.Reporter
	AutomationCheckResult  = automation.CheckResult
	AutomationService      = automation.Service

	Branch              = branch.Branch
	BranchStatus        = branch.Status
	BranchCreateOptions = branch.CreateOptions
	BranchService       = branch.Service

	DiffAction      = diffservice.Action
	DiffElementType = diffservice.ElementType
	DiffCounts      = diffservice.Counts
	DiffPeer        = diffservice.Peer
	DiffElement     = diffservice.Element
	DiffNode        = diffservice.Node
	DiffTree        = diffservice.Tree
	DiffOptions     = diffservice.Options
	DiffService     = diffservice.Service

	Node               = node.Node
	NodePage           = node.Page
	NodeMutationResult = node.MutationResult
	NodeService        = node.Service

	ObjectStoreService                     = objectstore.Service
	ObjectStoreUploadResult                = objectstore.UploadResult
	ObjectStoreUnsupportedContentTypeError = objectstore.UnsupportedContentTypeError

	Repository                    = repository.Repository
	RepositoryBranchState         = repository.BranchState
	RepositoryListOptions         = repository.ListOptions
	RepositoryUpdateCommitOptions = repository.UpdateCommitOptions
	RepositoryService             = repository.Service

	ResourcePoolAllocation        = resourcepool.Allocation
	ResourcePoolAddressOptions    = resourcepool.AddressOptions
	ResourcePoolPrefixOptions     = resourcepool.PrefixOptions
	ResourcePoolAllocatedOptions  = resourcepool.AllocatedOptions
	ResourcePoolAllocationPage    = resourcepool.AllocationPage
	ResourcePoolUtilization       = resourcepool.Utilization
	ResourcePoolUtilizationResult = resourcepool.UtilizationResult
	ResourcePoolMemberType        = resourcepool.MemberType
	ResourcePoolService           = resourcepool.Service

	TrackingGroup        = tracking.Group
	TrackingGroupOptions = tracking.GroupOptions
	TrackingGroupResult  = tracking.GroupResult

	TraversalNode             = traversal.Node
	TraversalRelationship     = traversal.Relationship
	TraversalHop              = traversal.Hop
	TraversalPath             = traversal.Path
	TraversalPathsResult      = traversal.PathsResult
	TraversalPathsOptions     = traversal.PathsOptions
	TraversalReachableNode    = traversal.ReachableNode
	TraversalReachableResult  = traversal.ReachableResult
	TraversalReachableOptions = traversal.ReachableOptions
	TraversalUnsupportedError = traversal.UnsupportedError
	TraversalService          = traversal.Service

	Task               = task.Task
	TaskState          = task.State
	TaskLog            = task.Log
	TaskRelatedNode    = task.RelatedNode
	TaskFilter         = task.Filter
	TaskListOptions    = task.ListOptions
	TaskPage           = task.Page
	TaskService        = task.Service
	TaskAmbiguousError = task.AmbiguousError

	SchemaService = schema.Service
)

const (
	AutomationSeverityInfo    = automation.SeverityInfo
	AutomationSeverityWarning = automation.SeverityWarning
	AutomationSeverityError   = automation.SeverityError
)

const (
	DiffActionAdded     = diffservice.ActionAdded
	DiffActionUpdated   = diffservice.ActionUpdated
	DiffActionRemoved   = diffservice.ActionRemoved
	DiffActionUnchanged = diffservice.ActionUnchanged
	DiffActionConflict  = diffservice.ActionConflict
)

const (
	DiffElementTypeAttribute        = diffservice.ElementTypeAttribute
	DiffElementTypeRelationshipOne  = diffservice.ElementTypeRelationshipOne
	DiffElementTypeRelationshipMany = diffservice.ElementTypeRelationshipMany
)

const (
	ResourcePoolMemberTypePrefix  = resourcepool.MemberTypePrefix
	ResourcePoolMemberTypeAddress = resourcepool.MemberTypeAddress
)

const (
	TaskStateScheduled  = task.StateScheduled
	TaskStatePending    = task.StatePending
	TaskStateRunning    = task.StateRunning
	TaskStateCompleted  = task.StateCompleted
	TaskStateFailed     = task.StateFailed
	TaskStateCancelled  = task.StateCancelled
	TaskStateCrashed    = task.StateCrashed
	TaskStatePaused     = task.StatePaused
	TaskStateCancelling = task.StateCancelling
)

const (
	BranchStatusOpen              = branch.StatusOpen
	BranchStatusNeedRebase        = branch.StatusNeedRebase
	BranchStatusNeedUpgradeRebase = branch.StatusNeedUpgradeRebase
	BranchStatusDeleting          = branch.StatusDeleting
	BranchStatusMerging           = branch.StatusMerging
	BranchStatusMerged            = branch.StatusMerged
)
