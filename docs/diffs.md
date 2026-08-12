# Diff summaries and trees

`Client.Diffs` retrieves changes calculated by Infrahub for a branch. The API
provides a concise node summary and a complete tree with global metadata.

## Node summary

```go
nodes, err := client.Diffs.Summary(ctx, infrahub.DiffOptions{
    Branch: "feature/inventory",
})
```

Each node contains its action, kind, ID, display label, counters, and changed
attributes or relationships. Cardinality-many relationships include peer-level
changes. An absent diff returns an empty non-nil slice.

## Complete tree

```go
tree, err := client.Diffs.Tree(ctx, infrahub.DiffOptions{
    Branch: "feature/inventory",
    Name:   "before-merge",
})
if err != nil {
    return err
}
if tree == nil {
    // Infrahub has no matching diff.
}
```

The tree includes base and diff branches, `time.Time` boundaries, additions,
updates, removals, conflicts, untracked changes, and the node summary.

Time-bounded retrieval uses GraphQL variables and validates the interval before
sending a request:

```go
tree, err := client.Diffs.Tree(ctx, infrahub.DiffOptions{
    Branch:   "feature/inventory",
    FromTime: from,
    ToTime:   to,
})
```

The branch is both the `DiffTree` argument and the request's schema/branch route.
Contextual trackers can override the stable `query-diff-tree` tracker. When
GraphQL returns partial data with errors, the decoded tree and error are both
returned.
