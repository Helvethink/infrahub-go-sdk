# Dynamic node queries

Infrahub query fields depend on the schema and branch. The `pkg/node` query builder lets callers select custom attributes and relationships and apply dynamic filters without interpolating values into GraphQL documents.

## Attributes and filters

```go
import "github.com/Helvethink/infrahub-go-sdk/pkg/node"

page, err := client.Nodes.Query(ctx, "BuiltinTag", node.QueryOptions{
    Branch: "main",
    Offset: 0,
    Limit:  100,
    Filters: []node.Filter{
        {Name: "name__value", Value: "staging"},
    },
    Selections: []node.Selection{
        node.Select("name", node.Select("value")),
        node.Select("description", node.Select("value")),
    },
})
```

Identity fields (`id`, `kind`, `hfid`, and `display_label`) are always selected. Other decoded values are available in `Node.Fields`:

```go
name := page.Nodes[0].Fields["name"].(map[string]any)["value"]
```

## Relationships

Selections may be nested to match the target branch's GraphQL schema:

```go
node.Select(
    "site",
    node.Select(
        "node",
        node.Select("id"),
        node.Select("display_label"),
    ),
)
```

Consult the SDL returned by `client.Schema.GraphQL` for the exact fields and nesting available on the selected branch.

## Filter types

The builder infers these common GraphQL types:

| Go value | GraphQL variable type |
| --- | --- |
| `string` | `String!` |
| `bool` | `Boolean!` |
| 32-bit-range integer | `Int!` |
| `float32`, `float64` | `Float!` |
| `time.Time` | `DateTime!` |
| `[]string` | `[String!]!` |
| `[]bool` | `[Boolean!]!` |
| `[]int` | `[Int!]!` |
| `[]float64` | `[Float!]!` |

Specify `Filter.Type` when the schema expects `ID`, `BigInt`, or another Infrahub/custom scalar:

```go
Filters: []node.Filter{
    {
        Name:  "member_of_groups__ids",
        Value: []string{"group-id"},
        Type:  "[ID!]!",
    },
    {
        Name:  "utilization__value",
        Value: int64(5_000_000_000),
        Type:  "BigInt!",
    },
}
```

The builder rejects invalid kinds, field names, filter names, duplicate arguments, malformed GraphQL types, unsupported inferred values, and integers outside GraphQL's signed 32-bit `Int` range.

Filter values are always transmitted through GraphQL variables. Selection names, filter names, and explicit types are validated before becoming part of the query document.
