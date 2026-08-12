# Go-native transforms, generators, and checks

`Client.Automation` provides extension points corresponding to the Python SDK's
Jinja transforms, generators, and checks. The Go SDK does not load Python modules
or embed a Jinja interpreter. Automation is compiled Go code with explicit
dependencies, contexts, errors, and types.

All three extension types can collect input from an Infrahub `CoreGraphQLQuery`
through `/api/query/{name}`.

## Named query collection

```go
var response struct {
    Data map[string]any `json:"data"`
}

err := client.Automation.Query(ctx, infrahub.AutomationQueryOptions{
    Name:      "device_configuration",
    Variables: map[string]any{"device": "edge-01"},
    Branch:    "main",
}, &response)
```

Query names are escaped as one URL segment. Branch, point-in-time, subscribers,
custom parameters, and `update_group` are request-scoped. The client's body-size,
authentication, redaction, tracker, and cancellation behavior still applies.

## Transforms

```go
rendered, err := client.Automation.RunTransform(ctx, infrahub.AutomationRunOptions{
    Query: infrahub.AutomationQueryOptions{
        Name:   "device_configuration",
        Branch: "main",
    },
}, func(ctx context.Context, data map[string]any) (any, error) {
    return renderDeviceConfiguration(data)
})
```

The callback receives the nested `data` object when the server returns a normal
GraphQL envelope, or the complete response otherwise. A transform can use
`text/template`, `html/template`, or another explicitly selected Go rendering
library. The SDK deliberately does not execute untrusted templates by default.

## Generators

Generators should be idempotent and use SDK services to create or update nodes:

```go
group, err := infrahub.NewTrackingGroup(infrahub.TrackingGroupOptions{
    Identifier: "inventory-generator",
    GroupKind:  "CoreGeneratorGroup",
    Branch:     "main",
})
if err != nil {
    return err
}

ctx = group.Context(ctx)
err = client.Automation.RunGenerator(ctx, infrahub.AutomationRunOptions{
    Query: infrahub.AutomationQueryOptions{Name: "generator_input", Branch: "main"},
}, func(ctx context.Context, data map[string]any) error {
    // Call client.Nodes.Create/Update or another SDK service.
    return generateInventory(ctx, client, data)
})
if err == nil {
    _, err = group.Save(ctx, client)
}
```

Tracking and persistence are explicit. Unlike the Python SDK, the runner never
deletes nodes that were not generated in a later run.

## Checks

```go
result, err := client.Automation.RunCheck(ctx, infrahub.AutomationRunOptions{
    Query: infrahub.AutomationQueryOptions{Name: "check_input", Branch: "main"},
}, func(ctx context.Context, data map[string]any, report *infrahub.AutomationReporter) error {
    report.Warning("description is empty", nodeID, "DcimDevice")
    report.Error("management address is missing", nodeID, "DcimDevice")
    return nil
})
```

`ERROR` findings fail a check; `WARNING` and `INFO` findings do not. Callback
execution errors are returned separately from validation failures. Reporters are
safe for concurrent use and findings carry severity, branch, object identity,
and sequence.
