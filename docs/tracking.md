# Tracking and group context

Tracking metadata is request-scoped in the Go SDK. It never changes shared
client state, so one client can safely execute concurrent workflows with
different trackers and groups.

## Tracker header

Use `tracking.WithTracker` to override the `X-Infrahub-Tracker` header for every
SDK operation using a context:

```go
ctx := tracking.WithTracker(ctx, "import-datacenter-zrh")
nodes, err := client.Nodes.List(ctx, "BuiltinTag", 0, 100, "main")
```

The contextual value takes precedence over a service's default tracker. Passing
an empty tracker suppresses the header for that context.

## Collect a group

Create a group, attach it to a context, then use that context for node operations:

```go
group, err := tracking.NewGroup(tracking.GroupOptions{
    Identifier:  "inventory-import",
    Params:      map[string]string{"site": "zrh"},
    Description: "Objects read or written by the inventory import",
    Branch:      "main",
})
if err != nil {
    return err
}

ctx = group.Context(ctx)
_, err = client.Nodes.Query(ctx, "BuiltinDevice", node.QueryOptions{
    Branch: "main",
})
if err != nil {
    return err
}

result, err := group.Save(ctx, client)
```

Node query, get, create, and update results are collected automatically.
`RecordNodeIDs` can add IDs returned by other services. Members are deduplicated,
sorted before persistence, and safe to collect from concurrent goroutines.

`Save` executes a `<GroupKind>Upsert` mutation and does nothing when the group is
empty. `CoreStandardGroup` is the default kind. Group names are deterministic:
the identifier is used directly without parameters and gains a stable digest
when parameters are present.

Unlike the Python SDK's `delete_unused_nodes` mode, saving a Go tracking group
does not delete objects that disappeared from a later run. Destructive cleanup
must be implemented explicitly by the application.
