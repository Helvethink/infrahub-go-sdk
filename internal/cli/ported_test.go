package cli

import (
	"bytes"
	"context"
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
