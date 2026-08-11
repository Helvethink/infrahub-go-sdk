package infrahub

import (
	"context"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/branch"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	"github.com/Helvethink/infrahub-go-sdk/pkg/repository"
	"github.com/Helvethink/infrahub-go-sdk/pkg/schema"
	"github.com/Helvethink/infrahub-go-sdk/pkg/tracking"
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

	Branch              = branch.Branch
	BranchStatus        = branch.Status
	BranchCreateOptions = branch.CreateOptions
	BranchService       = branch.Service

	Node               = node.Node
	NodePage           = node.Page
	NodeMutationResult = node.MutationResult
	NodeService        = node.Service

	Repository                    = repository.Repository
	RepositoryBranchState         = repository.BranchState
	RepositoryListOptions         = repository.ListOptions
	RepositoryUpdateCommitOptions = repository.UpdateCommitOptions
	RepositoryService             = repository.Service

	TrackingGroup        = tracking.Group
	TrackingGroupOptions = tracking.GroupOptions
	TrackingGroupResult  = tracking.GroupResult

	SchemaService = schema.Service
)

const (
	BranchStatusOpen              = branch.StatusOpen
	BranchStatusNeedRebase        = branch.StatusNeedRebase
	BranchStatusNeedUpgradeRebase = branch.StatusNeedUpgradeRebase
	BranchStatusDeleting          = branch.StatusDeleting
	BranchStatusMerging           = branch.StatusMerging
	BranchStatusMerged            = branch.StatusMerged
)
