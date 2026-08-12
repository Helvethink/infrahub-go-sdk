# Infrahub Go SDK

An idiomatic Go client and command-line tool for [Infrahub](https://www.infrahub.app/), inspired by the official Python SDK.

> This project is an early port. It currently provides the transport foundation, arbitrary GraphQL execution, branch management, schema APIs, and dynamic node mutations. Specialized Python SDK features are tracked in the roadmap below.

## Install

```sh
go get github.com/Helvethink/infrahub-go-sdk
```

## Client

```go
client, err := infrahub.NewClient(
    "https://infrahub.example.com",
    infrahub.WithAPIToken(os.Getenv("INFRAHUB_API_TOKEN")),
    infrahub.WithDefaultBranch("main"),
)
if err != nil {
    log.Fatal(err)
}

branches, err := client.Branches.List(context.Background())
```

All network operations accept `context.Context`. A client is safe for concurrent use, and branch selection is request-scoped.

## Packages

- `infrahub`: client facade and configuration
- `pkg/batch`: generic bounded concurrent execution
- `pkg/api`: low-level HTTP and GraphQL protocol
- `pkg/branch`: branch lifecycle and types
- `pkg/diff`: branch diff summaries and complete trees
- `pkg/schema`: schema discovery, validation, and loading
- `pkg/task`: background-task filtering and polling
- `pkg/config`: strict TOML and environment configuration
- `pkg/node`: generic operations for schema-defined objects
- `pkg/objectstore`: stored objects and text-file retrieval
- `pkg/repository`: repository discovery and commit tracking
- `pkg/resourcepool`: IP address/prefix allocation and utilization
- `pkg/tracking`: request trackers and group collection
- `cmd/infrahubctl`: executable entry point
- `internal/cli`: testable, non-public CLI implementation

Most applications should import only the root package. Packages under `pkg/` are available for deliberate advanced use; implementation details remain under `internal/`.

See the [Python SDK porting map](docs/compatibility.md) for implemented and planned capabilities.

## Development

```sh
make check
make race
make build
```

`make check` verifies formatting, runs `go vet`, runs `golangci-lint`, and executes all unit and facade tests. This check is mandatory after adding a feature.

## CLI

Build the command with `make build`, or install it directly:

```sh
go install github.com/Helvethink/infrahub-go-sdk/cmd/infrahubctl@latest
```

Configuration uses flags or environment variables:

```sh
export INFRAHUB_ADDRESS=https://infrahub.example.com
export INFRAHUB_API_TOKEN=...

infrahubctl branch list
infrahubctl branch create --description "SDK work" sdk-work
infrahubctl schema graphql > schema.graphql
printf 'query { Branch { name } }' | infrahubctl graphql
```

Run `infrahubctl help` for the complete initial command list.

TOML configuration is also supported from the platform user configuration directory, `INFRAHUB_CONFIG`, or `-config`. See the [configuration guide](docs/configuration.md) for the file format and precedence rules.

## Dynamic GraphQL

Infrahub generates a GraphQL schema for each data schema and branch. Use `Execute` for arbitrary queries:

```go
var result struct {
    Tags []struct {
        ID string `json:"id"`
    } `json:"BuiltinTag"`
}

err := client.Execute(ctx, infrahub.GraphQLRequest{
    Query: `query Tags { BuiltinTag { id } }`,
    OperationName: "Tags",
    Branch: "main",
}, &result)
```

If a GraphQL response contains both `data` and `errors`, data is decoded and the returned error can be inspected with `errors.As` as `*infrahub.GraphQLError`.

## Dynamic nodes

```go
tag, err := client.Nodes.Create(ctx, "BuiltinTag", map[string]any{
    "name":        map[string]any{"value": "staging"},
    "description": map[string]any{"value": "Staging resources"},
}, "main")
```

Dynamic filters and nested selections are available through `client.Nodes.Query`. See the [dynamic query guide](docs/dynamic-queries.md).

## Repositories

```go
repositories, err := client.Repositories.List(ctx, infrahub.RepositoryListOptions{
    Branches: []string{"main", "staging"},
})
```

Repository discovery aggregates commits and internal status across branches. Commit updates are also supported. See the [repository guide](docs/repositories.md).

## Tracking

```go
group, err := tracking.NewGroup(tracking.GroupOptions{Identifier: "inventory-import"})
ctx = group.Context(tracking.WithTracker(ctx, "inventory-import"))

_, err = client.Nodes.List(ctx, "BuiltinTag", 0, 100, "main")
result, err := group.Save(ctx, client)
```

Tracking is request-scoped and safe for concurrent workflows. See the [tracking and group-context guide](docs/tracking.md).

## Object and file storage

```go
uploaded, err := client.ObjectStore.Upload(ctx, "generated configuration")
content, err := client.ObjectStore.Get(ctx, uploaded.Identifier)
file, err := client.ObjectStore.GetFileByID(ctx, nodeID)
```

Stored objects and text-file endpoints preserve base paths, escape identifiers, and honor response-size limits. See the [object-store guide](docs/object-store.md).

## Tasks

```go
tasks, err := client.Tasks.All(ctx, infrahub.TaskListOptions{
    Filter: infrahub.TaskFilter{States: []infrahub.TaskState{
        infrahub.TaskStateRunning,
    }},
})

task, err := client.Tasks.Wait(ctx, taskID, time.Second)
```

Task filters, logs, related nodes, pagination and cancellation-aware polling are supported. See the [tasks guide](docs/tasks.md).

## Batches

```go
results, err := batch.Map(ctx, nodeIDs, func(ctx context.Context, id string) (*infrahub.Node, error) {
    return client.Nodes.GetByID(ctx, "BuiltinDevice", id, "main")
}, batch.Options{Concurrency: 5})
```

Batch results retain input indexes and support fail-fast or per-result error collection. See the [batches guide](docs/batches.md).

## IP address and prefix pools

```go
prefixLength := 32
address, err := client.ResourcePools.AllocateAddress(ctx, infrahub.ResourcePoolAddressOptions{
    PoolID:       poolID,
    Identifier:   "loopback-edge-01",
    PrefixLength: &prefixLength,
    AddressKind:  "IpamIPAddress",
    Branch:       "main",
})
```

Prefix allocation, allocation history and utilization are also supported. See the [resource-pool guide](docs/resource-pools.md).

## Diffs

```go
tree, err := client.Diffs.Tree(ctx, infrahub.DiffOptions{
    Branch: "feature/inventory",
})
```

Node summaries, complete metadata, time ranges, attributes, relationships and peer changes are supported. See the [diff guide](docs/diffs.md).

## Current scope

- GraphQL transport, authentication, trackers, branch/time routing, and partial errors
- Branch list/get/create/delete/rebase/validate/merge and diff data
- Schema fetch, SDL export, validation, and loading
- Generic node create/update/delete
- Generic node list/get-by-ID/get-by-HFID with offset pagination
- Repository discovery across branches and protected commit updates
- Request-scoped tracker overrides and concurrent tracking groups
- Stored object upload/download and text-file retrieval by storage ID, node ID, or HFID
- Background-task filtering, pagination, lookup, counts, logs and polling
- Generic bounded batches with cancellation and configurable error collection
- IP address/prefix allocation, allocation history and pool utilization
- Branch diff summaries and complete diff trees

Planned ports include schema-aware custom-field query construction, graph traversal, diffs, IP resource allocation, object/file storage, tasks, batches, and tracking. Python-only runtime features such as Jinja transforms and pytest plugins will not be copied into the core Go library.
