# Graph traversal

`Client.Traversal` exposes Infrahub 1.10+ path traversal and reachable-node
queries. Results contain lightweight identities; use `Client.Nodes.GetByID` or
`Query` when schema-specific attributes are needed.

## Find paths

```go
maxDepth := 8
result, err := client.Traversal.Paths(ctx, infrahub.TraversalPathsOptions{
    SourceID:      sourceID,
    DestinationID: destinationID,
    MaxDepth:      &maxDepth,
    KindFilter:    []string{"DcimDevice", "DcimInterface"},
    Branch:        "main",
})
```

Paths contain ordered hops. The first hop is the source and has a nil
relationship. Other hops describe both relationship sides and the relationship
kind. Optional filters support relationship identifiers, excluded namespaces,
excluded kinds, and kinds re-included from Infrahub's defaults.

Pointer options preserve server defaults and allow an explicit `false`:

```go
shortestOnly := false
options.ShortestPathsOnly = &shortestOnly
```

`PathExists` sets `max_paths` to one and is the inexpensive convenience method
for connectivity checks.

```go
exists, err := client.Traversal.PathExists(ctx, options)
```

## Find reachable nodes

```go
maxResults := 100
result, err := client.Traversal.Reachable(ctx, infrahub.TraversalReachableOptions{
    SourceID:    deviceID,
    TargetKinds: []string{"LocationSite", "OrganizationTenant"},
    MaxResults:  &maxResults,
    Branch:      "main",
})
```

Each dependency includes the terminal node, its depth, and the full path from
the source. `At` on either options type selects a point-in-time graph without
mutating the shared client.

The SDK validates documented server limits before sending requests. A server
without traversal fields returns `*traversal.UnsupportedError` with minimum
version 1.10. Runtime GraphQL errors such as an unknown source node remain
`*api.GraphQLError`. Partial results are preserved for non-version errors.

Nodes encountered in traversal results are automatically recorded when the
request uses a tracking-group context.
