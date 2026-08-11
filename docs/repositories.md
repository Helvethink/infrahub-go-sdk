# Repositories

`Client.Repositories` exposes Infrahub repositories without requiring callers to
construct GraphQL operations. It follows the Python SDK behavior while returning
stable Go values instead of mutable Python nodes.

## Discover repositories

```go
repositories, err := client.Repositories.List(ctx, infrahub.RepositoryListOptions{})
if err != nil {
    return err
}

for _, repository := range repositories {
    stagingBranch, found := repository.StagingBranch()
    // repository.Branches contains the commit and internal status per branch.
}
```

With no branches specified, `List` discovers every Infrahub branch, queries them
with bounded concurrency, follows offset pagination, aggregates repositories by
name, and returns them sorted by name. Limit discovery when only a few branches
matter:

```go
repositories, err := client.Repositories.List(ctx, infrahub.RepositoryListOptions{
    Branches:    []string{"main", "staging"},
    Concurrency: 2,
})
```

The default query kind is `CoreGenericRepository`. Set `Kind` to a concrete or
compatible repository kind when required by the target schema.

## Update a commit

```go
commit, err := client.Repositories.UpdateCommit(ctx, infrahub.RepositoryUpdateCommitOptions{
    Branch:       "main",
    RepositoryID: repositoryID,
    Commit:       "19f4478b",
    ReadOnly:     true,
})
```

`ReadOnly` selects `CoreReadOnlyRepositoryUpdate`; otherwise the service uses
`CoreRepositoryUpdate`. Identifiers and commit values are always GraphQL variables.

The operations were verified against the Infrahub schema associated with the
Python SDK 1.22.2 compatibility inventory. A custom server schema remains the
source of truth when kinds or fields differ between versions.
