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
