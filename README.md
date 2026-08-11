# Infrahub Go SDK

An idiomatic, dependency-free Go client for [Infrahub](https://www.infrahub.app/), inspired by the official Python SDK.

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
- `api`: low-level HTTP and GraphQL protocol
- `branch`: branch lifecycle and types
- `schema`: schema discovery, validation, and loading
- `node`: generic operations for schema-defined objects

Most applications should import only the root package. Domain packages are available when their types or constructors are needed directly.

See the [Python SDK porting map](docs/compatibility.md) for implemented and planned capabilities.

## Development

```sh
make check
make race
```

`make check` verifies formatting, runs `go vet`, and executes all unit and facade tests.

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

## Current scope

- GraphQL transport, authentication, trackers, branch/time routing, and partial errors
- Branch list/get/create/delete/rebase/validate/merge and diff data
- Schema fetch, SDL export, validation, and loading
- Generic node create/update/delete
- Generic node list/get-by-ID/get-by-HFID with offset pagination

Planned ports include schema-aware custom-field query construction, graph traversal, diffs, IP resource allocation, object/file storage, tasks, batches, and tracking. Python-only runtime features such as Jinja transforms and pytest plugins will not be copied into the core Go library.
