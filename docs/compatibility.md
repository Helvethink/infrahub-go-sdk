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
| `dump`, `load`, `menu`, `marketplace`, `protocols`, `telemetry`, `validate` | Not ported yet; these need dedicated server contracts or local file-format implementations before exposing stable Go commands |

The Go CLI accepts Python-compatible configuration aliases for day-to-day migration: `server_address` in TOML, `INFRAHUB_DEFAULT_BRANCH`, and `INFRAHUBCTL_CONFIG`.

Compatibility means equivalent server behavior where practical. It does not imply identical names, mutable node objects, separate sync/async clients, Python decorators, or runtime code loading.
