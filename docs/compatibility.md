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
| graph traversal | Planned | dedicated traversal service |
| diff summary/tree | Planned | dedicated diff service |
| IP address/prefix allocation | Planned | resource-pool service |
| batches | Planned | Go concurrency helper |
| tasks | Planned | task service |
| object/file store | Planned | object-store service |
| tracking/group context | Planned | immutable request options |
| repositories | Planned | repository service |
| Jinja transforms/generators/checks | Not core | provide Go-native extension points |
| `infrahubctl` core commands | Available | `cmd/infrahubctl` |

Compatibility means equivalent server behavior where practical. It does not imply identical names, mutable node objects, separate sync/async clients, Python decorators, or runtime code loading.
