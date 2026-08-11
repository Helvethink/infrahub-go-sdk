# Batches

Package `pkg/batch` executes independent operations with bounded concurrency.
It is generic, has no dependency on a specific Infrahub resource, and propagates
`context.Context` to every job.

## Map inputs

`Map` is the simplest choice when the same operation applies to every input:

```go
results, err := batch.Map(ctx, nodeIDs, func(ctx context.Context, id string) (*infrahub.Node, error) {
    return client.Nodes.GetByID(ctx, "BuiltinDevice", id, "main")
}, batch.Options{Concurrency: 5})
if err != nil {
    return err
}

for _, result := range results {
    node := result.Value
    _ = node
}
```

Results are sorted by input index even when operations finish in another order.
The concurrency default is five, matching the Python SDK default.

## Run jobs

Use `Run` when each operation needs its own closure:

```go
jobs := []batch.Job[string]{
    func(ctx context.Context) (string, error) {
        return client.ObjectStore.Get(ctx, "artifact-a")
    },
    func(ctx context.Context) (string, error) {
        return client.ObjectStore.Get(ctx, "artifact-b")
    },
}

results, err := batch.Run(ctx, jobs, batch.Options{Concurrency: 2})
```

## Error policies

The default is fail-fast. The first failing job cancels the internal context,
no additional jobs are scheduled, and `Run` returns `*batch.Error`. Operations
already running are awaited, so jobs must honor context cancellation.

Set `ContinueOnError` for the Python SDK's `return_exceptions` behavior:

```go
results, err := batch.Map(ctx, inputs, worker, batch.Options{
    Concurrency:     5,
    ContinueOnError: true,
})
if err != nil {
    // A parent context cancellation still returns a top-level error.
    return err
}

for _, result := range results {
    if result.Err != nil {
        // Handle this input's failure.
    }
}
```

With `ContinueOnError`, every input is attempted and job errors are stored in
their corresponding results. Parent context cancellation always remains a
top-level error.
