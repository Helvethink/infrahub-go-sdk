package infrahub

import (
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/branch"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	"github.com/Helvethink/infrahub-go-sdk/pkg/repository"
	"github.com/Helvethink/infrahub-go-sdk/pkg/schema"
)

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
