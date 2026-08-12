# IP address and prefix pools

`Client.ResourcePools` provides typed access to Infrahub IP address and prefix
allocation. Allocation mutations return the stable resource descriptor supplied
by Infrahub: ID, kind, identifier, and display label.

## Allocate an address

```go
prefixLength := 32
address, err := client.ResourcePools.AllocateAddress(ctx, infrahub.ResourcePoolAddressOptions{
    PoolID:       poolID,
    Identifier:   "loopback-edge-01",
    PrefixLength: &prefixLength,
    AddressKind:  "IpamIPAddress",
    Data: map[string]any{
        "description": "Edge router loopback",
    },
    Branch: "main",
})
```

`Identifier` enables Infrahub's idempotent allocation behavior. Pointer prefix
lengths distinguish an omitted server default from an explicit value, including
zero. Prefix lengths outside 0–128 are rejected before a request is sent.

## Allocate a prefix

```go
prefixLength := 31
prefix, err := client.ResourcePools.AllocatePrefix(ctx, infrahub.ResourcePoolPrefixOptions{
    PoolID:       poolID,
    Identifier:   "interconnect-edge-01-edge-02",
    PrefixLength: &prefixLength,
    MemberType:   infrahub.ResourcePoolMemberTypeAddress,
    PrefixKind:   "IpamIPPrefix",
    Branch:       "main",
})
```

Member type may be `prefix`, `address`, or omitted to use the pool default.
All allocation data is sent through GraphQL variables.

The allocation response deliberately avoids a second dynamic-schema request.
Use the returned kind and ID with `Client.Nodes.Query` or `GetByID` when custom
attributes such as `address` or `prefix` are required.

## Allocations and utilization

```go
allocations, err := client.ResourcePools.AllAllocated(ctx, infrahub.ResourcePoolAllocatedOptions{
    PoolID:     poolID,
    ResourceID: backingPrefixID,
    Branch:     "main",
})

usage, err := client.ResourcePools.Utilization(ctx, poolID, "main")
```

`Allocated` retrieves one offset-based page; `AllAllocated` follows every page.
`Utilization` returns aggregate usage plus usage and weight for each backing
resource. Results returned by allocation operations participate in a tracking
group when its context is used.
