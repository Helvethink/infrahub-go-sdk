# Tasks

`Client.Tasks` provides typed access to Infrahub background tasks. It supports
filtering, offset pagination, lookup by ID, counts, optional logs and related
nodes, and cancellation-aware polling.

## List and filter

```go
page, err := client.Tasks.List(ctx, infrahub.TaskListOptions{
    Filter: infrahub.TaskFilter{
        Branch:    "main",
        States:    []infrahub.TaskState{infrahub.TaskStateRunning},
        Workflows: []string{"schema-update"},
    },
    Limit:               100,
    IncludeLogs:         true,
    IncludeRelatedNodes: true,
})
```

The available filters match the stable `InfrahubTask` arguments: IDs, free-text
query, branch, states, workflows, and related-node IDs. All caller values are
sent as GraphQL variables.

`List` retrieves one page and defaults to 50 entries. `All` follows every page;
for `All`, `Limit` is the page size and `Offset` is the first item to retrieve.

```go
tasks, err := client.Tasks.All(ctx, infrahub.TaskListOptions{
    Filter: infrahub.TaskFilter{States: []infrahub.TaskState{
        infrahub.TaskStateFailed,
        infrahub.TaskStateCrashed,
    }},
})
```

## Get, count, and wait

```go
count, err := client.Tasks.Count(ctx, infrahub.TaskFilter{Branch: "main"})

task, err := client.Tasks.Get(ctx, taskID, true, true)

completed, err := client.Tasks.Wait(ctx, taskID, time.Second)
```

`Wait` returns for `COMPLETED`, `FAILED`, `CANCELLED`, or `CRASHED`. Its timeout
comes from the context, following normal Go cancellation semantics:

```go
ctx, cancel := context.WithTimeout(ctx, time.Minute)
defer cancel()

task, err := client.Tasks.Wait(ctx, taskID, time.Second)
```

A missing task returns `*api.NotFoundError`. Multiple results for one ID return
`*task.AmbiguousError`. A final failed state is returned as a task rather than a
Go error, allowing callers to inspect task logs and state.
