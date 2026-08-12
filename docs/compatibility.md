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
| diff summary/tree | Available | `Client.Diffs` and `pkg/diff` |
| IP address/prefix allocation | Available | `Client.ResourcePools` and `pkg/resourcepool` |
| batches | Available | `pkg/batch.Run` and `pkg/batch.Map` |
| tasks | Available | `Client.Tasks` and `pkg/task` |
| object/file store | Available | `Client.ObjectStore` and `pkg/objectstore` |
| tracking/group context | Available | `tracking.WithTracker`, `tracking.Group` |
| repositories | Available | `Client.Repositories` and `pkg/repository` |
| Jinja transforms/generators/checks | Not core | provide Go-native extension points |
| `infrahubctl` core commands | Available | `cmd/infrahubctl` |

Compatibility means equivalent server behavior where practical. It does not imply identical names, mutable node objects, separate sync/async clients, Python decorators, or runtime code loading.
