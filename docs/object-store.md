# Object and file storage

`Client.ObjectStore` provides the object and file-storage operations exposed by
Infrahub. Every method accepts `context.Context`, preserves configured base-paths,
escapes identifiers as individual URL segments, and uses the client's response
size limit.

## Upload and retrieve content

```go
uploaded, err := client.ObjectStore.Upload(ctx, "generated configuration")
if err != nil {
    return err
}

content, err := client.ObjectStore.Get(ctx, uploaded.Identifier)
```

`Upload` returns a typed value containing the Infrahub storage identifier and
checksum. `Get` mirrors the Python SDK object endpoint and does not impose a MIME
type restriction on its response.

## Retrieve file objects

```go
byStorageID, err := client.ObjectStore.GetFileByStorageID(ctx, storageID)
byNodeID, err := client.ObjectStore.GetFileByID(ctx, nodeID)
byHFID, err := client.ObjectStore.GetFileByHFID(
    ctx,
    "CoreFile",
    []string{"configuration", "edge-01"},
)
```

File methods accept `text/*`, JSON, YAML, and `application/x-yaml`. Other MIME
types return `*objectstore.UnsupportedContentTypeError`, which callers can inspect
with `errors.As`. This matches the Python SDK's text-file contract; use the object
endpoint when the response must be handled without this validation.

## Track requests

Service methods provide stable default trackers. Override or suppress them for a
workflow with the request-scoped tracking API:

```go
ctx := infrahub.WithTracker(ctx, "render-device-config")
content, err := client.ObjectStore.GetFileByID(ctx, nodeID)
```
