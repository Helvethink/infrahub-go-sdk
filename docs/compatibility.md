# Python SDK porting map

The reference implementation is [`opsmill/infrahub-sdk-python`](https://github.com/opsmill/infrahub-sdk-python), inventoried at version 1.22.2. This project follows Go conventions rather than duplicating the Python object model.

| Python SDK capability | Go status | Go API |
| --- | --- | --- |
| `execute_graphql` | Available | `Client.Execute` |
| API-token authentication | Available | `WithAPIToken` |
| TOML and environment configuration | Available | `pkg/config` and `infrahubctl` |
| default/request branch and `at` | Available | `WithDefaultBranch`, `GraphQLRequest` |
| branch manager | Available | `Client.Branches` |
| schema fetch/load/check/SDL | Available | `Client.Schema` |
| generic node create/update/delete | Available | `Client.Nodes` |
| identity-only `get`, `all`, pagination | Available | `GetByID`, `GetByHFID`, `List` |
| dynamic attribute filters/selections | Available | `node.Service.Query` |
| graph traversal | Available | `Client.Traversal` and `pkg/traversal` |
| diff summary/tree | Available | `Client.Diffs` and `pkg/diff` |
| IP address/prefix allocation | Available | `Client.ResourcePools` and `pkg/resourcepool` |
| batches | Available | `pkg/batch.Run` and `pkg/batch.Map` |
| tasks | Available | `Client.Tasks` and `pkg/task` |
| telemetry snapshots | Available | `Client.Telemetry` and `pkg/telemetry` |
| object/file store | Available | `Client.ObjectStore` and `pkg/objectstore` |
| tracking/group context | Available | `tracking.WithTracker`, `tracking.Group` |
| repositories | Available | `Client.Repositories` and `pkg/repository` |
| Jinja transforms/generators/checks | Go-native | `Client.Automation` and `pkg/automation` extension points |
| `infrahubctl` core commands | Partial | `cmd/infrahubctl` |

## `infrahubctl` command compatibility

The Python CLI command list was checked against the Infrahub documentation in August 2026. The Go CLI implements commands where the Go SDK has an equivalent native service and avoids pretending to support Python runtime features.

| Python `infrahubctl` command | Go status |
| --- | --- |
| `branch list/get/create/delete/rebase/validate/merge/report` | Available |
| `graphql` | Available |
| `info` | Available |
| `object get/create/update/delete/load/validate` | Available |
| `repository list` | Available |
| `schema graphql/load/check/export/list/show` | Available |
| `task list` | Available |
| `version` | Available |
| `check`, `generator`, `render`, `run`, `transform` | Not ported; Python-runtime/Jinja execution is intentionally not embedded in the Go CLI |
| `dump`, `load` | Available; Python 1.22.2-compatible `nodes.json` LDJSON and `relationships.json` files, with explicit overwrite protection. Loading into another branch creates missing nodes with new IDs and remaps dumped relationships to those IDs. |
| `menu load/validate` | Available for `infrahub.app/v1` Menu documents; missing paths and order weights are derived deterministically |
| `marketplace list/search/show/get` | Available against Marketplace API v1; schema downloads and collection metadata are supported, while dependency-tree expansion remains intentionally explicit rather than automatic |
| `protocols` | Available; generates deterministic Python `Protocol` classes from a remote branch schema or local `--schema` documents |
| `telemetry list/export` | Available through `Client.Telemetry`; export paginates until the server-reported count is reached |
| `validate schema/graphql-query` | Available; schema validation checks the portable document structure locally and GraphQL validation executes the document against the selected branch |

The Go CLI accepts Python-compatible configuration aliases for day-to-day migration: `server_address` in TOML, `INFRAHUB_DEFAULT_BRANCH`, and `INFRAHUBCTL_CONFIG`.

Compatibility means equivalent server behavior where practical. It does not imply identical names, mutable node objects, separate sync/async clients, Python decorators, or runtime code loading.

The transfer, menu, marketplace, protocols, and telemetry contracts above were
implemented against the Python SDK 1.22.2 formats and endpoints. Local schema
validation deliberately checks the portable envelope and structure; semantic
validation against a particular Infrahub version remains `schema check`.
