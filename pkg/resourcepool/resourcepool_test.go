package resourcepool_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
	"github.com/Helvethink/infrahub-go-sdk/pkg/resourcepool"
)

type executorFunc func(context.Context, api.GraphQLRequest, any) error

func (f executorFunc) Execute(ctx context.Context, request api.GraphQLRequest, dst any) error {
	return f(ctx, request, dst)
}

func decode(dst any, payload string) error { return json.Unmarshal([]byte(payload), dst) }

func intPtr(value int) *int { return &value }

func TestAllocateAddressUsesTypedInputVariable(t *testing.T) {
	t.Parallel()
	service := resourcepool.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.OperationName != "InfrahubIPAddressPoolGetResource" || request.Branch != "main" || !strings.Contains(request.Query, "$data: IPAddressPoolGetResourceInput!") {
			t.Fatalf("request = %#v", request)
		}
		if strings.Contains(request.Query, "pool-id") || strings.Contains(request.Query, "loopback") {
			t.Fatalf("values interpolated in query: %s", request.Query)
		}
		data := request.Variables["data"].(map[string]any)
		want := map[string]any{"id": "pool-id", "identifier": "loopback", "prefix_length": 32, "address_type": "IpamIPAddress", "data": map[string]any{"description": "SDK"}}
		if !reflect.DeepEqual(data, want) {
			t.Fatalf("data = %#v, want %#v", data, want)
		}
		return decode(dst, `{"InfrahubIPAddressPoolGetResource":{"ok":true,"node":{"id":"address-id","kind":"IpamIPAddress","identifier":"loopback","display_label":"192.0.2.1/32"}}}`)
	}))
	allocation, err := service.AllocateAddress(context.Background(), resourcepool.AddressOptions{
		PoolID: "pool-id", Identifier: "loopback", PrefixLength: intPtr(32), AddressKind: "IpamIPAddress",
		Data: map[string]any{"description": "SDK"}, Branch: "main",
	})
	if err != nil || allocation.ID != "address-id" || allocation.DisplayLabel != "192.0.2.1/32" {
		t.Fatalf("AllocateAddress() = %#v, %v", allocation, err)
	}
}

func TestAllocatePrefixAndOperationFailure(t *testing.T) {
	t.Parallel()
	service := resourcepool.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if !strings.Contains(request.Query, "$data: IPPrefixPoolGetResourceInput!") {
			t.Fatalf("query = %s", request.Query)
		}
		data := request.Variables["data"].(map[string]any)
		if data["member_type"] != resourcepool.MemberTypeAddress || data["prefix_length"] != 31 {
			t.Fatalf("data = %#v", data)
		}
		return decode(dst, `{"InfrahubIPPrefixPoolGetResource":{"ok":false,"node":null}}`)
	}))
	_, err := service.AllocatePrefix(context.Background(), resourcepool.PrefixOptions{
		PoolID: "pool-id", PrefixLength: intPtr(31), MemberType: resourcepool.MemberTypeAddress,
	})
	var operationError *api.OperationError
	if !errors.As(err, &operationError) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestAllocatedAndAllAllocatedPagination(t *testing.T) {
	t.Parallel()
	var offsets []int
	service := resourcepool.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		offset := request.Variables["offset"].(int)
		offsets = append(offsets, offset)
		identifier := "one"
		if offset > 0 {
			identifier = "two"
		}
		return decode(dst, `{"InfrahubResourcePoolAllocated":{"count":2,"edges":[{"node":{"id":"`+identifier+`-id","kind":"IpamIPAddress","branch":"main","identifier":"`+identifier+`","display_label":"192.0.2.1/32"}}]}}`)
	}))
	allocations, err := service.AllAllocated(context.Background(), resourcepool.AllocatedOptions{
		PoolID: "pool", ResourceID: "prefix", Limit: 1,
	})
	if err != nil || len(allocations) != 2 || !reflect.DeepEqual(offsets, []int{0, 1}) {
		t.Fatalf("AllAllocated() = %#v, %v, offsets=%#v", allocations, err, offsets)
	}
}

func TestUtilizationDecodesAggregateAndResources(t *testing.T) {
	t.Parallel()
	service := resourcepool.NewService(executorFunc(func(_ context.Context, request api.GraphQLRequest, dst any) error {
		if request.Variables["poolID"] != "pool" || request.Branch != "feature/ipam" {
			t.Fatalf("request = %#v", request)
		}
		return decode(dst, `{"InfrahubResourcePoolUtilization":{"count":1,"utilization":93.75,"utilization_branches":1.25,"utilization_default_branch":92.5,"edges":[{"node":{"id":"prefix","kind":"IpamIPPrefix","display_label":"192.0.2.0/24","utilization":93.75,"utilization_branches":1.25,"utilization_default_branch":92.5,"weight":10}}]}}`)
	}))
	result, err := service.Utilization(context.Background(), "pool", "feature/ipam")
	if err != nil || result.Count != 1 || result.Utilization != 93.75 || len(result.Resources) != 1 || result.Resources[0].Weight != 10 {
		t.Fatalf("Utilization() = %#v, %v", result, err)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	service := resourcepool.NewService(nil)
	checks := []func() error{
		func() error {
			_, err := service.AllocateAddress(context.Background(), resourcepool.AddressOptions{})
			return err
		},
		func() error {
			_, err := service.AllocateAddress(context.Background(), resourcepool.AddressOptions{PoolID: "pool", PrefixLength: intPtr(129)})
			return err
		},
		func() error {
			_, err := service.AllocatePrefix(context.Background(), resourcepool.PrefixOptions{PoolID: "pool", MemberType: "host"})
			return err
		},
		func() error {
			_, err := service.Allocated(context.Background(), resourcepool.AllocatedOptions{PoolID: "pool"})
			return err
		},
		func() error { _, err := service.Utilization(context.Background(), "", ""); return err },
	}
	for _, check := range checks {
		if err := check(); err == nil {
			t.Fatal("validation error = nil")
		}
	}
}
