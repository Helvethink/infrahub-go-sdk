// Package resourcepool provides IP address and prefix allocation operations.
package resourcepool

import (
	"context"
	"fmt"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const defaultPageSize = 50

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service manages Infrahub resource-pool allocations.
type Service struct{ client Executor }

// NewService creates a resource-pool service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// Allocation identifies a resource allocated by a pool.
type Allocation struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Identifier   *string `json:"identifier"`
	DisplayLabel string  `json:"display_label"`
	Branch       string  `json:"branch,omitempty"`
}

// AddressOptions configures allocation from a CoreIPAddressPool.
type AddressOptions struct {
	PoolID       string
	Identifier   string
	PrefixLength *int
	AddressKind  string
	Data         map[string]any
	Branch       string
}

// MemberType describes the members accepted by an allocated prefix.
type MemberType string

const (
	MemberTypePrefix  MemberType = "prefix"
	MemberTypeAddress MemberType = "address"
)

// PrefixOptions configures allocation from a CoreIPPrefixPool.
type PrefixOptions struct {
	PoolID       string
	Identifier   string
	PrefixLength *int
	MemberType   MemberType
	PrefixKind   string
	Data         map[string]any
	Branch       string
}

// AllocatedOptions configures one page of allocation records.
type AllocatedOptions struct {
	PoolID     string
	ResourceID string
	Branch     string
	Offset     int
	Limit      int
}

// AllocationPage is one offset-based page of allocation records.
type AllocationPage struct {
	Count       int64
	Offset      int
	Limit       int
	Allocations []Allocation
}

// Utilization describes usage of one resource backing a pool.
type Utilization struct {
	ID                       string  `json:"id"`
	Kind                     string  `json:"kind"`
	DisplayLabel             string  `json:"display_label"`
	Utilization              float64 `json:"utilization"`
	UtilizationBranches      float64 `json:"utilization_branches"`
	UtilizationDefaultBranch float64 `json:"utilization_default_branch"`
	Weight                   int64   `json:"weight"`
}

// UtilizationResult describes a pool and each backing resource's utilization.
type UtilizationResult struct {
	Count                    int64
	Utilization              float64
	UtilizationBranches      float64
	UtilizationDefaultBranch float64
	Resources                []Utilization
}

// AllocateAddress allocates an IP address from a CoreIPAddressPool.
func (s *Service) AllocateAddress(ctx context.Context, options AddressOptions) (*Allocation, error) {
	if options.PoolID == "" {
		return nil, fmt.Errorf("infrahub: IP address pool ID must not be empty")
	}
	if err := validatePrefixLength(options.PrefixLength); err != nil {
		return nil, err
	}
	data := map[string]any{"id": options.PoolID}
	setOptional(data, "identifier", options.Identifier)
	setOptional(data, "prefix_length", options.PrefixLength)
	setOptional(data, "address_type", options.AddressKind)
	setOptional(data, "data", options.Data)
	return s.allocate(ctx, "InfrahubIPAddressPoolGetResource", "IPAddressPoolGetResourceInput", data, options.Branch, "allocate-ip-address")
}

// AllocatePrefix allocates an IP prefix from a CoreIPPrefixPool.
func (s *Service) AllocatePrefix(ctx context.Context, options PrefixOptions) (*Allocation, error) {
	if options.PoolID == "" {
		return nil, fmt.Errorf("infrahub: IP prefix pool ID must not be empty")
	}
	if err := validatePrefixLength(options.PrefixLength); err != nil {
		return nil, err
	}
	if options.MemberType != "" && options.MemberType != MemberTypePrefix && options.MemberType != MemberTypeAddress {
		return nil, fmt.Errorf("infrahub: invalid IP prefix member type %q", options.MemberType)
	}
	data := map[string]any{"id": options.PoolID}
	setOptional(data, "identifier", options.Identifier)
	setOptional(data, "prefix_length", options.PrefixLength)
	setOptional(data, "member_type", options.MemberType)
	setOptional(data, "prefix_type", options.PrefixKind)
	setOptional(data, "data", options.Data)
	return s.allocate(ctx, "InfrahubIPPrefixPoolGetResource", "IPPrefixPoolGetResourceInput", data, options.Branch, "allocate-ip-prefix")
}

func (s *Service) allocate(ctx context.Context, operation, inputType string, input map[string]any, branch, tracker string) (*Allocation, error) {
	var response map[string]struct {
		OK   bool        `json:"ok"`
		Node *Allocation `json:"node"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: ` + inputType + `!) { ` + operation + `(data: $data) { ok node { id kind identifier display_label } } }`,
		Variables: map[string]any{"data": input}, OperationName: operation, Branch: branch, Tracker: tracker,
	}, &response)
	result := response[operation]
	if result.Node != nil {
		requestcontext.RecordNodeIDs(ctx, result.Node.ID)
	}
	if err != nil {
		return result.Node, err
	}
	if !result.OK || result.Node == nil {
		return nil, &api.OperationError{Operation: operation}
	}
	return result.Node, nil
}

// Allocated returns one page of resources allocated for a pool and backing resource.
func (s *Service) Allocated(ctx context.Context, options AllocatedOptions) (*AllocationPage, error) {
	if options.PoolID == "" || options.ResourceID == "" {
		return nil, fmt.Errorf("infrahub: pool ID and resource ID must not be empty")
	}
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("infrahub: allocation offset and limit must not be negative")
	}
	limit := options.Limit
	if limit == 0 {
		limit = defaultPageSize
	}
	var response struct {
		Result struct {
			Count int64 `json:"count"`
			Edges []struct {
				Node Allocation `json:"node"`
			} `json:"edges"`
		} `json:"InfrahubResourcePoolAllocated"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:         `query ResourcePoolAllocated($poolID: String!, $resourceID: String!, $offset: Int!, $limit: Int!) { InfrahubResourcePoolAllocated(pool_id: $poolID, resource_id: $resourceID, offset: $offset, limit: $limit) { count edges { node { id kind branch identifier display_label } } } }`,
		Variables:     map[string]any{"poolID": options.PoolID, "resourceID": options.ResourceID, "offset": options.Offset, "limit": limit},
		OperationName: "ResourcePoolAllocated", Branch: options.Branch, Tracker: "get-allocated-resources",
	}, &response)
	page := &AllocationPage{Count: response.Result.Count, Offset: options.Offset, Limit: limit, Allocations: make([]Allocation, 0, len(response.Result.Edges))}
	ids := make([]string, 0, len(response.Result.Edges))
	for _, edge := range response.Result.Edges {
		page.Allocations = append(page.Allocations, edge.Node)
		ids = append(ids, edge.Node.ID)
	}
	requestcontext.RecordNodeIDs(ctx, ids...)
	return page, err
}

// AllAllocated retrieves every allocation for a pool and backing resource.
func (s *Service) AllAllocated(ctx context.Context, options AllocatedOptions) ([]Allocation, error) {
	all := make([]Allocation, 0)
	for {
		page, err := s.Allocated(ctx, options)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Allocations...)
		if int64(options.Offset+len(page.Allocations)) >= page.Count || len(page.Allocations) == 0 {
			return all, nil
		}
		options.Offset += page.Limit
	}
}

// Utilization returns aggregate and per-resource utilization for a pool.
func (s *Service) Utilization(ctx context.Context, poolID, branch string) (*UtilizationResult, error) {
	if poolID == "" {
		return nil, fmt.Errorf("infrahub: resource pool ID must not be empty")
	}
	var response struct {
		Result struct {
			Count                    int64   `json:"count"`
			Utilization              float64 `json:"utilization"`
			UtilizationBranches      float64 `json:"utilization_branches"`
			UtilizationDefaultBranch float64 `json:"utilization_default_branch"`
			Edges                    []struct {
				Node Utilization `json:"node"`
			} `json:"edges"`
		} `json:"InfrahubResourcePoolUtilization"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:     `query ResourcePoolUtilization($poolID: String!) { InfrahubResourcePoolUtilization(pool_id: $poolID) { count utilization utilization_branches utilization_default_branch edges { node { id kind display_label utilization utilization_branches utilization_default_branch weight } } } }`,
		Variables: map[string]any{"poolID": poolID}, OperationName: "ResourcePoolUtilization", Branch: branch,
		Tracker: "get-pool-utilization",
	}, &response)
	result := response.Result
	output := &UtilizationResult{
		Count: result.Count, Utilization: result.Utilization, UtilizationBranches: result.UtilizationBranches,
		UtilizationDefaultBranch: result.UtilizationDefaultBranch, Resources: make([]Utilization, 0, len(result.Edges)),
	}
	for _, edge := range result.Edges {
		output.Resources = append(output.Resources, edge.Node)
	}
	return output, err
}

func validatePrefixLength(value *int) error {
	if value != nil && (*value < 0 || *value > 128) {
		return fmt.Errorf("infrahub: IP prefix length must be between 0 and 128")
	}
	return nil
}

func setOptional(data map[string]any, key string, value any) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			data[key] = typed
		}
	case MemberType:
		if typed != "" {
			data[key] = typed
		}
	case *int:
		if typed != nil {
			data[key] = *typed
		}
	case map[string]any:
		if len(typed) > 0 {
			data[key] = typed
		}
	}
}
