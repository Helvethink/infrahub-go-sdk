package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
)

func TestGenerateProtocolsIsDeterministic(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"nodes": []any{
		map[string]any{"kind": "InfraDevice", "attributes": []any{
			map[string]any{"name": "zeta"}, map[string]any{"name": "alpha"},
		}},
	}}
	result, err := generateProtocols(schema, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "class InfraDevice(Protocol):") || strings.Index(result, "alpha") > strings.Index(result, "zeta") {
		t.Fatalf("generated source = %q", result)
	}
}

func TestMarketplaceSearchUsesMarketplaceAPIWithoutInfrahubConfig(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/schemas" || request.URL.Query().Get("search") != "edge" {
			t.Errorf("request URL = %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"marketplace", "search", "--url", server.URL, "edge",
	})
	if code != 0 || !strings.Contains(stdout.String(), `"items": []`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDumpSchemaHelpers(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"nodes": []any{
		map[string]any{"namespace": "Infra", "name": "Device", "kind": "InfraDevice", "attributes": []any{map[string]any{"name": "name"}}},
		map[string]any{"namespace": "Internal", "name": "Hidden", "kind": "InternalHidden"},
	}}
	if got := schemaNodeKinds(schema, nil, nil); !reflect.DeepEqual(got, []string{"InfraDevice"}) {
		t.Fatalf("kinds = %#v", got)
	}
	selections, err := dumpSelections(schema, "InfraDevice")
	if err != nil || len(selections) != 1 || selections[0].Name != "name" {
		t.Fatalf("selections=%#v err=%v", selections, err)
	}
}

func TestDumpSelectionsBuildsSortedDynamicFields(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"generics": []any{
		"invalid",
		map[string]any{
			"namespace": "Core", "name": "Asset",
			"attributes": []any{
				"invalid",
				map[string]any{"name": "zeta"},
				map[string]any{"name": ""},
			},
			"relationships": []any{
				"invalid",
				map[string]any{"name": "owner", "cardinality": "one"},
				map[string]any{"name": "members", "cardinality": "many"},
				map[string]any{"name": ""},
			},
		},
	}}
	want := []node.Selection{
		node.Select("members", node.Select("edges", node.Select("node", node.Select("id")))),
		node.Select("owner", node.Select("node", node.Select("id"))),
		node.Select("zeta", node.Select("value")),
	}
	got, err := dumpSelections(schema, "CoreAsset")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("selections=%#v, want %#v, err=%v", got, want, err)
	}
}

func TestLoadCreatesMissingDumpNodeAndNormalizesRelationships(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	record := dumpRecord{
		ID:   "node-id",
		Kind: "BuiltinTag",
		GraphQLJSON: `{"id":"node-id","kind":"BuiltinTag","hfid":["tag"],"display_label":"tag",` +
			`"name":{"value":"tag"},"groups":{"edges":[{"node":{"id":"group-id","kind":"CoreGroup"}}]}}`,
	}
	nodes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nodes.json"), append(nodes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "relationships.json"), []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		requests++
		switch payload.OperationName {
		case "GetBuiltinTagByID":
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTag":{"count":0,"edges":[]}}}`))
		case "GetBuiltinTagByHFID":
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTag":{"count":0,"edges":[]}}}`))
		case "BuiltinTagCreate":
			data, _ := payload.Variables["data"].(map[string]any)
			if data["id"] != nil || data["hfid"] != nil || data["display_label"] != nil || data["groups"] != nil {
				t.Errorf("create data = %#v", data)
			}
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTagCreate":{"ok":true,"object":{"id":"new-node-id","kind":"BuiltinTag","hfid":["tag"],"display_label":"tag"}}}}`))
		case "BuiltinTagUpsert":
			data, _ := payload.Variables["data"].(map[string]any)
			if data["id"] != "new-node-id" {
				t.Errorf("upsert data = %#v", data)
			}
			groups, _ := data["groups"].([]any)
			if len(groups) != 1 || groups[0].(map[string]any)["id"] != "group-id" {
				t.Errorf("groups = %#v", data["groups"])
			}
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTagUpsert":{"ok":true,"object":{"id":"new-node-id","kind":"BuiltinTag","hfid":["tag"],"display_label":"tag"}}}}`))
		default:
			t.Errorf("unexpected operation %q", payload.OperationName)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", server.URL, "load", "--directory", directory,
	})
	if code != 0 || requests != 4 || !strings.Contains(stdout.String(), `"loaded": 1`) {
		t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests, stdout.String(), stderr.String())
	}
}

func TestRemapRelatedNodeIDs(t *testing.T) {
	t.Parallel()
	input := []map[string]any{{"id": "old-id"}, {"id": "external-id"}}
	result, ok := remapRelatedNodeIDs(input, map[string]string{"old-id": "new-id"}).([]map[string]any)
	if !ok || !reflect.DeepEqual(result, []map[string]any{{"id": "new-id"}, {"id": "external-id"}}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestMenuValidateAndLocalProtocolsNeedNoServer(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	menuPath := filepath.Join(directory, "menu.yaml")
	if err := os.WriteFile(menuPath, []byte("apiVersion: infrahub.app/v1\nkind: Menu\nspec:\n  data:\n    - namespace: Main\n      name: devices\n      label: Devices\n      kind: InfraDevice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	if code := runner.Run(context.Background(), []string{"menu", "validate", menuPath}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"valid": true`) {
		t.Fatalf("stdout=%q", stdout.String())
	}

	schemaPath := filepath.Join(directory, "schema.yaml")
	if err := os.WriteFile(schemaPath, []byte("nodes:\n  - namespace: Infra\n    name: Device\n    attributes:\n      - name: hostname\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"protocols", "--schema", schemaPath, "--out", "-"}); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "class InfraDevice(Protocol):") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestPrepareMenuItemDerivesDefaults(t *testing.T) {
	t.Parallel()
	item := map[string]any{"namespace": "Main", "name": "devices", "label": "Devices", "kind": "InfraDevice"}
	if err := prepareMenuItem(item, 1); err != nil {
		t.Fatal(err)
	}
	if item["path"] != "/objects/InfraDevice" || item["order_weight"] != 2000 {
		t.Fatalf("item = %#v", item)
	}
}

func TestTelemetryListAndExport(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/telemetry/snapshots" {
			t.Errorf("path = %q", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("start_date") != "2026-08-01T00:00:00Z" || query.Get("end_date") != "2026-08-02T00:00:00Z" {
			t.Errorf("query = %q", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"snapshots":[{"metric":"value"}],"count":1}`))
	}))
	defer server.Close()
	directory := t.TempDir()
	out := filepath.Join(directory, "telemetry.json")
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	code := runner.Run(context.Background(), []string{
		"-address", server.URL, "telemetry", "list",
		"--start", "2026-08-01T00:00:00Z", "--end", "2026-08-02T00:00:00Z", "--limit", "1",
	})
	if code != 0 || !strings.Contains(stdout.String(), `"metric": "value"`) {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runner.Run(context.Background(), []string{
		"-address", server.URL, "telemetry", "export", "--start", "2026-08-01T00:00:00Z",
		"--end", "2026-08-02T00:00:00Z", "--out", out,
	})
	data, err := os.ReadFile(out)
	if code != 0 || err != nil || !strings.Contains(string(data), `"metric": "value"`) {
		t.Fatalf("export code=%d data=%q readErr=%v stderr=%q", code, data, err, stderr.String())
	}
}

func TestTelemetryValidationErrors(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"-address", "https://example.com", "telemetry", "list", "--start", "invalid"},
		{"-address", "https://example.com", "telemetry", "list", "--end", "invalid"},
		{"-address", "https://example.com", "telemetry", "unknown"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := testRunner(&stdout, &stderr).Run(context.Background(), args); code != 2 {
			t.Errorf("args=%q code=%d stderr=%q", args, code, stderr.String())
		}
	}
}

func TestValidateSchemaAndGraphQLQuery(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	schemaPath := filepath.Join(directory, "schema.yaml")
	if err := os.WriteFile(schemaPath, []byte("nodes:\n  - kind: InfraDevice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	if code := runner.Run(context.Background(), []string{"validate", "schema", schemaPath}); code != 0 || !strings.Contains(stdout.String(), `"valid": true`) {
		t.Fatalf("schema code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	queryPath := filepath.Join(directory, "query.graphql")
	if err := os.WriteFile(queryPath, []byte("query Example($enabled: Boolean!) { value }"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		if request.URL.EscapedPath() != "/graphql/feature" || payload.Variables["enabled"] != true {
			t.Errorf("path=%q variables=%#v", request.URL.EscapedPath(), payload.Variables)
		}
		_, _ = writer.Write([]byte(`{"data":{"value":42}}`))
	}))
	defer server.Close()
	out := filepath.Join(directory, "result.json")
	stdout.Reset()
	stderr.Reset()
	code := runner.Run(context.Background(), []string{
		"-address", server.URL, "-branch", "feature", "validate", "graphql-query", queryPath,
		"--variable", "enabled=true", "--out", out,
	})
	data, err := os.ReadFile(out)
	if code != 0 || err != nil || !strings.Contains(string(data), `"value": 42`) {
		t.Fatalf("query code=%d data=%q readErr=%v stderr=%q", code, data, err, stderr.String())
	}
}

func TestDumpRelationshipHelpers(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"nodes": []any{
		map[string]any{
			"namespace": "Infra", "name": "Device",
			"attributes": []any{map[string]any{"name": "name"}},
			"relationships": []any{
				map[string]any{"name": "interfaces", "identifier": "device_interfaces", "peer": "InfraInterface", "cardinality": "many", "optional": true},
				map[string]any{"name": "site", "identifier": "device_site", "peer": "LocationSite", "cardinality": "one"},
			},
		},
		map[string]any{"kind": "InfraInterface", "namespace": "Infra"},
		map[string]any{"kind": "LocationSite", "namespace": "Location"},
	}}
	if got := schemaNodeKinds(schema, []string{"Infra"}, []string{"InfraInterface"}); !reflect.DeepEqual(got, []string{"InfraDevice"}) {
		t.Fatalf("filtered kinds = %#v", got)
	}
	if got := manyRelationshipIdentifiers(schema, []string{"InfraDevice", "InfraInterface"}); !reflect.DeepEqual(got, []string{"device_interfaces"}) {
		t.Fatalf("relationship identifiers = %#v", got)
	}
	names := relationshipNamesByKind(schema)
	if names["InfraDevice\x00device_interfaces"] != "interfaces" || names["InfraDevice\x00device_site"] != "site" {
		t.Fatalf("relationship names = %#v", names)
	}
	selections, err := dumpSelections(schema, "InfraDevice")
	if err != nil || len(selections) != 3 {
		t.Fatalf("selections=%#v err=%v", selections, err)
	}
	if _, err := dumpSelections(schema, "MissingKind"); err == nil {
		t.Fatal("missing kind error = nil")
	}
}

func TestManyRelationshipIdentifiersFiltersDuplicatesAndSorts(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"nodes": []any{
		map[string]any{
			"namespace": "Infra", "name": "Device",
			"relationships": []any{
				map[string]any{"identifier": "z_links", "peer": "InfraInterface", "cardinality": "many", "optional": true},
				map[string]any{"identifier": "z_links", "peer": "InfraInterface", "cardinality": "many", "optional": true},
				map[string]any{"identifier": "a_links", "peer": "InfraInterface", "cardinality": "many", "optional": true},
				map[string]any{"identifier": "required_links", "peer": "InfraInterface", "cardinality": "many", "optional": false},
				map[string]any{"identifier": "missing_peer", "peer": "InfraMissing", "cardinality": "many", "optional": true},
				map[string]any{"identifier": "", "peer": "InfraInterface", "cardinality": "many", "optional": true},
			},
		},
		map[string]any{"kind": "InfraInterface"},
	}}
	want := []string{"a_links", "z_links"}
	if got := manyRelationshipIdentifiers(schema, []string{"InfraDevice", "InfraInterface"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("relationship identifiers = %#v, want %#v", got, want)
	}
}

func TestPrepareMenuItemValidation(t *testing.T) {
	t.Parallel()
	if err := prepareMenuItem(map[string]any{"name": "item", "label": "Item"}, 0); err == nil {
		t.Fatal("missing namespace error = nil")
	}
	item := map[string]any{
		"namespace": "Main", "name": "parent", "label": "Parent",
		"children": []any{map[string]any{"namespace": "Main", "name": "child", "label": "Child"}},
	}
	if err := prepareMenuItem(item, 0); err != nil {
		t.Fatal(err)
	}
	children := item["children"].([]any)
	if children[0].(map[string]any)["order_weight"] != 1000 {
		t.Fatalf("children = %#v", children)
	}
	item["children"] = []any{map[string]any{"namespace": "Main", "name": "child"}}
	if err := prepareMenuItem(item, 0); err == nil || !strings.Contains(err.Error(), "label must be") {
		t.Fatalf("child validation error = %v", err)
	}
	item["children"] = []any{"invalid"}
	if err := prepareMenuItem(item, 0); err == nil {
		t.Fatal("invalid child error = nil")
	}
}

func TestDumpExportsNodesAndRelationships(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/schema":
			if request.URL.Query().Get("branch") != "feature" {
				t.Errorf("schema branch = %q", request.URL.Query().Get("branch"))
			}
			_, _ = writer.Write([]byte(`{"nodes":[{"kind":"BuiltinTag","namespace":"Builtin","attributes":[{"name":"name"}]}]}`))
		case request.Method == http.MethodPost:
			payload := decodeCLIRequest(t, request)
			if payload.OperationName != "QueryBuiltinTag" || !strings.Contains(payload.Query, "name { value }") {
				t.Fatalf("query = %#v", payload)
			}
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTag":{"count":1,"edges":[{"node":{"id":"tag-id","kind":"BuiltinTag","hfid":["tag"],"display_label":"tag","name":{"value":"tag"}}}]}}}`))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", server.URL, "dump", "--branch", "feature", "--directory", directory,
	})
	if code != 0 || !strings.Contains(stdout.String(), `"nodes": 1`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	nodes, err := os.ReadFile(filepath.Join(directory, "nodes.json"))
	if err != nil || !strings.Contains(string(nodes), `"id":"tag-id"`) || !strings.Contains(string(nodes), `\"name\":{\"value\":\"tag\"}`) {
		t.Fatalf("nodes=%q err=%v", nodes, err)
	}
	relationships, err := os.ReadFile(filepath.Join(directory, "relationships.json"))
	if err != nil || strings.TrimSpace(string(relationships)) != "[]" {
		t.Fatalf("relationships=%q err=%v", relationships, err)
	}
}

func TestDumpPaginatesNodes(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"nodes":[{"kind":"BuiltinTag","namespace":"Builtin","attributes":[{"name":"name"}]}]}`))
			return
		}
		payload := decodeCLIRequest(t, request)
		if payload.OperationName != "QueryBuiltinTag" || payload.Variables["limit"] != float64(1) {
			t.Fatalf("payload = %#v", payload)
		}
		switch payload.Variables["offset"] {
		case float64(0):
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTag":{"count":2,"edges":[{"node":{"id":"tag-1","kind":"BuiltinTag","name":{"value":"one"}}}]}}}`))
		case float64(1):
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTag":{"count":2,"edges":[{"node":{"id":"tag-2","kind":"BuiltinTag","name":{"value":"two"}}}]}}}`))
		default:
			t.Fatalf("offset = %#v", payload.Variables["offset"])
		}
		requests++
	}))
	defer server.Close()

	stdout, stderr, code := runCLI(t, server, "dump", "--directory", directory, "--limit", "1")
	nodes, err := os.ReadFile(filepath.Join(directory, "nodes.json"))
	if code != 0 || err != nil || requests != 2 || strings.Count(string(nodes), `"graphql_json"`) != 2 {
		t.Fatalf("code=%d requests=%d nodes=%q readErr=%v stdout=%q stderr=%q", code, requests, nodes, err, stdout, stderr)
	}
}

func TestDumpRejectsUnsafeInputsAndExistingOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "invalid limit", args: []string{"-address", "https://example.com", "dump", "--limit", "0"}},
		{name: "internal namespace", args: []string{"-address", "https://example.com", "dump", "--namespace", "Internal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := testRunner(&stdout, &stderr).Run(context.Background(), tt.args); code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "nodes.json"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", "https://example.com", "dump", "--directory", directory,
	})
	if code != 1 || !strings.Contains(stderr.String(), "refusing to overwrite") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestMenuLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "menu.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: infrahub.app/v1\nkind: Menu\nspec:\n  kind: CoreMenuItem\n  data:\n    - namespace: Main\n      name: devices\n      label: Devices\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		if payload.OperationName != "CoreMenuItemUpsert" {
			t.Fatalf("operation = %q", payload.OperationName)
		}
		_, _ = writer.Write([]byte(`{"data":{"CoreMenuItemUpsert":{"ok":true,"object":{"id":"menu-id","kind":"CoreMenuItem","hfid":["Main","devices"],"display_label":"Devices"}}}}`))
	}))
	defer server.Close()
	stdout, stderr, code := runCLI(t, server, "menu", "load", path)
	if code != 0 || !strings.Contains(stdout, `"loaded": 1`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestMarketplaceListCollectionAndDownload(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/collections":
			if request.URL.Query().Get("limit") != "2" {
				t.Errorf("query = %q", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"items":[{"name":"collection"}]}`))
		case "/api/v1/schemas/Infra/Device/download":
			_, _ = writer.Write([]byte("schema: device\n"))
		default:
			t.Fatalf("path = %q", request.URL.Path)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	if code := runner.Run(context.Background(), []string{"marketplace", "list", "--url", server.URL, "--collection", "--limit", "2"}); code != 0 || !strings.Contains(stdout.String(), `"collection"`) {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := filepath.Join(t.TempDir(), "device.yaml")
	stdout.Reset()
	stderr.Reset()
	if code := runner.Run(context.Background(), []string{"marketplace", "get", "--url", server.URL, "--out", out, "Infra/Device"}); code != 0 {
		t.Fatalf("get code=%d stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "schema: device\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestGenerateProtocolsTypesAndErrors(t *testing.T) {
	t.Parallel()
	schema := map[string]any{"nodes": []any{map[string]any{
		"namespace": "Infra", "name": "Device",
		"attributes": []any{
			map[string]any{"name": "enabled", "kind": "Boolean"},
			map[string]any{"name": "created_at", "kind": "DateTime", "optional": true},
			map[string]any{"name": "metadata", "kind": "JSON"},
		},
		"relationships": []any{
			map[string]any{"name": "interfaces", "peer": "InfraInterface", "cardinality": "many", "optional": true},
		},
	}}}
	result, err := generateProtocols(schema, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# Synchronous SDK mode requested.", "created_at: datetime | None", "enabled: bool", "metadata: Any", "interfaces: Sequence[InfraInterface] | None"} {
		if !strings.Contains(result, expected) {
			t.Errorf("generated source missing %q: %s", expected, result)
		}
	}
	if _, err := generateProtocols(map[string]any{}, false); err == nil {
		t.Fatal("empty schema error = nil")
	}
}

func TestLoadRestoresExistingNodesAndRelationshipEdges(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	records := []dumpRecord{
		{ID: "old-device", Kind: "InfraDevice", GraphQLJSON: `{"id":"old-device","kind":"InfraDevice","name":{"value":"edge-01"}}`},
		{Kind: "BuiltinTag", GraphQLJSON: `{"name":{"value":"staging"}}`},
	}
	var nodes bytes.Buffer
	encoder := json.NewEncoder(&nodes)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "nodes.json"), nodes.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	relationships := `[{"node":{"identifier":"device_site","peers":[{"id":"old-device","kind":"InfraDevice"},{"id":"external-site","kind":"LocationSite"}]}}]`
	if err := os.WriteFile(filepath.Join(directory, "relationships.json"), []byte(relationships), 0o600); err != nil {
		t.Fatal(err)
	}
	deviceUpserts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/api/schema" {
			_, _ = writer.Write([]byte(`{"nodes":[{"kind":"InfraDevice","relationships":[{"name":"site","identifier":"device_site"}]}]}`))
			return
		}
		payload := decodeCLIRequest(t, request)
		switch payload.OperationName {
		case "GetInfraDeviceByID":
			_, _ = writer.Write([]byte(`{"data":{"InfraDevice":{"count":1,"edges":[{"node":{"id":"old-device","kind":"InfraDevice","hfid":["edge-01"],"display_label":"edge-01"}}]}}}`))
		case "InfraDeviceUpsert":
			deviceUpserts++
			data := payload.Variables["data"].(map[string]any)
			if data["id"] != "old-device" {
				t.Errorf("device data = %#v", data)
			}
			if deviceUpserts == 2 {
				sites := data["site"].([]any)
				if sites[0].(map[string]any)["id"] != "external-site" {
					t.Errorf("sites = %#v", sites)
				}
			}
			_, _ = writer.Write([]byte(`{"data":{"InfraDeviceUpsert":{"ok":true,"object":{"id":"old-device","kind":"InfraDevice","hfid":["edge-01"],"display_label":"edge-01"}}}}`))
		case "BuiltinTagUpsert":
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTagUpsert":{"ok":true,"object":{"id":"new-tag","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}}}`))
		default:
			t.Fatalf("unexpected operation %q", payload.OperationName)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", server.URL, "load", "--branch", "feature", "--directory", directory,
	})
	if code != 0 || deviceUpserts != 2 || !strings.Contains(stdout.String(), `"loaded": 2`) {
		t.Fatalf("code=%d upserts=%d stdout=%q stderr=%q", code, deviceUpserts, stdout.String(), stderr.String())
	}
}

func TestLoadContinuesAfterRejectedNode(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	records := []dumpRecord{
		{Kind: "BadNode", GraphQLJSON: `{"name":{"value":"bad"}}`},
		{Kind: "BuiltinTag", GraphQLJSON: `{"name":{"value":"good"}}`},
	}
	var nodes bytes.Buffer
	encoder := json.NewEncoder(&nodes)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(directory, "nodes.json"), nodes.String())
	writeTestFile(t, filepath.Join(directory, "relationships.json"), "[]")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		switch payload.OperationName {
		case "BadNodeUpsert":
			_, _ = writer.Write([]byte(`{"errors":[{"message":"rejected"}]}`))
		case "BuiltinTagUpsert":
			_, _ = writer.Write([]byte(`{"data":{"BuiltinTagUpsert":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag"}}}}`))
		default:
			t.Fatalf("unexpected operation %q", payload.OperationName)
		}
	}))
	defer server.Close()

	stdout, stderr, code := runCLI(t, server, "load", "--directory", directory, "--continue-on-error")
	if code != 1 || !strings.Contains(stdout, `"loaded": 1`) || !strings.Contains(stdout, `"failed": 1`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestLoadContinuesAfterInvalidRelationshipEdge(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "nodes.json"), "")
	writeTestFile(t, filepath.Join(directory, "relationships.json"),
		`[{"node":{"identifier":"device_site","peers":[{"id":"device-id","kind":"InfraDevice"}]}}]`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/schema" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL)
		}
		_, _ = writer.Write([]byte(`{"nodes":[]}`))
	}))
	defer server.Close()

	stdout, stderr, code := runCLI(t, server, "load", "--directory", directory, "--continue-on-error")
	if code != 1 || !strings.Contains(stdout, `"loaded": 0`) || !strings.Contains(stdout, `"failed": 1`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestLoadInputErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		nodes         string
		relationships string
		want          string
	}{
		{name: "invalid relationships", nodes: "", relationships: "not-json", want: "decode relationships.json"},
		{name: "invalid record", nodes: "not-json\n", relationships: "[]", want: "decode nodes.json"},
		{name: "invalid node data", nodes: `{"id":"one","kind":"InfraDevice","graphql_json":"not-json"}` + "\n", relationships: "[]", want: "decode node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "nodes.json"), []byte(tt.nodes), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "relationships.json"), []byte(tt.relationships), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
				"-address", "https://example.com", "load", "--directory", directory,
			})
			if code != 1 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestDumpExportsManyRelationships(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"nodes":[{"kind":"InfraDevice","namespace":"Infra","relationships":[{"name":"interfaces","identifier":"device_interfaces","peer":"InfraInterface","cardinality":"many","optional":true}]},{"kind":"InfraInterface","namespace":"Infra"}]}`))
			return
		}
		payload := decodeCLIRequest(t, request)
		switch payload.OperationName {
		case "QueryInfraDevice":
			_, _ = writer.Write([]byte(`{"data":{"InfraDevice":{"count":0,"edges":[]}}}`))
		case "QueryInfraInterface":
			_, _ = writer.Write([]byte(`{"data":{"InfraInterface":{"count":0,"edges":[]}}}`))
		case "":
			identifiers := payload.Variables["relationship_identifiers"].([]any)
			if len(identifiers) != 1 || identifiers[0] != "device_interfaces" {
				t.Errorf("identifiers = %#v", identifiers)
			}
			_, _ = writer.Write([]byte(`{"data":{"Relationship":{"edges":[{"node":{"identifier":"device_interfaces","peers":[{"id":"device-id","kind":"InfraDevice"},{"id":"interface-id","kind":"InfraInterface"}]}}]}}}`))
		default:
			t.Fatalf("operation = %q", payload.OperationName)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", server.URL, "dump", "--directory", directory, "--namespace", "Infra",
	})
	data, err := os.ReadFile(filepath.Join(directory, "relationships.json"))
	if code != 0 || err != nil || !strings.Contains(string(data), "device_interfaces") {
		t.Fatalf("code=%d relationships=%q readErr=%v stderr=%q", code, data, err, stderr.String())
	}
}

func TestProtocolsFetchesRemoteSchemaAndWritesFile(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/schema" || request.URL.Query().Get("branch") != "feature" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		_, _ = writer.Write([]byte(`{"generics":[{"kind":"CoreArtifact","attributes":[{"name":"name","kind":"Text"}]}]}`))
	}))
	defer server.Close()
	out := filepath.Join(t.TempDir(), "protocols.py")
	var stdout, stderr bytes.Buffer
	code := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", server.URL, "protocols", "--branch", "feature", "--out", out,
	})
	data, err := os.ReadFile(out)
	if code != 0 || err != nil || !strings.Contains(string(data), "class CoreArtifact(Protocol):") {
		t.Fatalf("code=%d source=%q readErr=%v stderr=%q", code, data, err, stderr.String())
	}
}

func TestValidateSchemaFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "empty document", data: "{}\n", want: "no schema documents found"},
		{name: "missing collections", data: "version: '1.0'\n", want: "must define nodes or generics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "schema.yaml")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := testRunner(&stdout, &stderr).Run(context.Background(), []string{"validate", "schema", path})
			if code != 1 || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestPortedCommandUsageErrors(t *testing.T) {
	t.Parallel()
	menuPath := filepath.Join(t.TempDir(), "menu.yaml")
	if err := os.WriteFile(menuPath, []byte("apiVersion: infrahub.app/v1\nkind: Menu\nspec:\n  data:\n    - namespace: Main\n      name: item\n      label: Item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func(Runner) int
	}{
		{name: "load positional argument", run: func(r Runner) int { return r.runLoad(context.Background(), nil, "main", []string{"extra"}) }},
		{name: "menu missing arguments", run: func(r Runner) int { return r.runMenu(context.Background(), nil, "main", nil) }},
		{name: "menu unknown command", run: func(r Runner) int { return r.runMenu(context.Background(), nil, "main", []string{"unknown", menuPath}) }},
		{name: "telemetry missing command", run: func(r Runner) int { return r.runTelemetry(context.Background(), nil, nil) }},
		{name: "validate missing arguments", run: func(r Runner) int { return r.runValidate(context.Background(), nil, "main", nil) }},
		{name: "validate unknown command", run: func(r Runner) int {
			return r.runValidate(context.Background(), nil, "main", []string{"unknown", "file"})
		}},
		{name: "protocols positional argument", run: func(r Runner) int { return r.runProtocols(context.Background(), nil, "main", []string{"extra"}) }},
		{name: "marketplace missing command", run: func(r Runner) int { return r.runMarketplace(context.Background(), nil) }},
		{name: "marketplace invalid URL", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"list", "--url", "://"}) }},
		{name: "marketplace list positional", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"list", "extra"}) }},
		{name: "marketplace empty search", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"search"}) }},
		{name: "marketplace show missing identifier", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"show"}) }},
		{name: "marketplace malformed identifier", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"show", "invalid"}) }},
		{name: "marketplace unknown command", run: func(r Runner) int { return r.runMarketplace(context.Background(), []string{"unknown"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := tt.run(testRunner(&stdout, &stderr)); code != 2 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}
