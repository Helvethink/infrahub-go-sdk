package infrahub

import (
	"context"
	"fmt"
	"net/http"

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
	"github.com/Helvethink/infrahub-go-sdk/pkg/telemetry"
	"github.com/Helvethink/infrahub-go-sdk/pkg/traversal"
)

// Client is the high-level Infrahub client. It is safe for concurrent use.
// Its services share immutable configuration and one HTTP connection pool.
type Client struct {
	protocol *api.Client

	// Branches provides branch lifecycle operations.
	Branches *branch.Service
	// Automation provides transform, generator, check, and named-query operations.
	Automation *automation.Service
	// Diffs provides branch diff summaries and trees.
	Diffs *diffservice.Service
	// Schema provides schema discovery, validation, and loading operations.
	Schema *schema.Service
	// Nodes provides generic operations for schema-defined nodes.
	Nodes *node.Service
	// Repositories provides repository discovery and commit operations.
	Repositories *repository.Service
	// ResourcePools provides resource allocation and utilization operations.
	ResourcePools *resourcepool.Service
	// ObjectStore provides stored-object and text-file operations.
	ObjectStore *objectstore.Service
	// Tasks provides background-task lookup, filtering, and polling operations.
	Tasks *task.Service
	// Telemetry provides telemetry snapshot operations.
	Telemetry *telemetry.Service
	// Traversal provides graph path and reachable-node operations.
	Traversal *traversal.Service
}

// NewClient creates an Infrahub client for address.
func NewClient(address string, options ...Option) (*Client, error) {
	cfg := config{
		httpClient:   defaultHTTPClient(),
		branch:       defaultBranch,
		userAgent:    defaultUserAgent,
		headers:      make(http.Header),
		maxBodyBytes: defaultMaxBodyBytes,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("infrahub: option must not be nil")
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}
	protocol, err := api.NewClient(address, api.Config{
		HTTPClient: cfg.httpClient, Token: cfg.token, DefaultBranch: cfg.branch,
		UserAgent: cfg.userAgent, Headers: cfg.headers, MaxBodyBytes: cfg.maxBodyBytes,
	})
	if err != nil {
		return nil, err
	}
	client := &Client{protocol: protocol}
	client.Branches = branch.NewService(protocol)
	client.Automation = automation.NewService(protocol)
	client.Diffs = diffservice.NewService(protocol)
	client.Schema = schema.NewService(protocol)
	client.Nodes = node.NewService(protocol)
	client.Repositories = repository.NewService(protocol)
	client.ResourcePools = resourcepool.NewService(protocol)
	client.ObjectStore = objectstore.NewService(protocol)
	client.Tasks = task.NewService(protocol)
	client.Telemetry = telemetry.NewService(protocol)
	client.Traversal = traversal.NewService(protocol)
	return client, nil
}

// DefaultBranch returns the client's default branch.
func (c *Client) DefaultBranch() string { return c.protocol.DefaultBranch() }

// Execute runs an arbitrary GraphQL operation. If GraphQL returns both data
// and errors, Execute decodes data into dst and returns *api.GraphQLError.
func (c *Client) Execute(ctx context.Context, request GraphQLRequest, dst any) error {
	return c.protocol.Execute(ctx, request, dst)
}
