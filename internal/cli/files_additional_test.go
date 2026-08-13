package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStructuredFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	jsonPath := filepath.Join(directory, "data.json")
	yamlPath := filepath.Join(directory, "schema.yaml")
	ignoredPath := filepath.Join(directory, "ignored.txt")
	writeTestFile(t, jsonPath, `{"name":"tag","count":2}`)
	writeTestFile(t, yamlPath, "version: '1.0'\nnodes: []\n")
	writeTestFile(t, ignoredPath, "ignored")

	files, err := expandStructuredFiles([]string{directory})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !isStructuredFile(jsonPath) || !isStructuredFile(yamlPath) || isStructuredFile(ignoredPath) {
		t.Fatalf("files = %#v", files)
	}
	data, err := readDataFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if data["count"] != json.Number("2") {
		t.Fatalf("data = %#v", data)
	}
	schemas, err := readSchemaDocuments([]string{directory})
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) != 2 {
		t.Fatalf("schemas = %#v", schemas)
	}
}

func TestReadObjectJSONAndNormalization(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "objects.json")
	writeTestFile(t, path, `{
  "apiVersion":"infrahub.app/v1",
  "kind":"Object",
  "spec":{"kind":"BuiltinTag","data":[{
    "id":"id-1",
    "hfid":["tag"],
    "name":{"value":"tag"},
    "enabled":true,
    "peers":["a","b"],
    "children":[]
  }]}
}`)
	documents, err := readObjectDocuments([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	data := normalizeObjectData(documents[0].Data[0])
	if data["id"] != "id-1" {
		t.Fatalf("data = %#v", data)
	}
	if _, ok := data["name"].(map[string]any); !ok {
		t.Fatalf("name = %#v", data["name"])
	}
	if data["enabled"].(map[string]any)["value"] != true {
		t.Fatalf("enabled = %#v", data["enabled"])
	}
	if _, ok := data["peers"].([]any); !ok {
		t.Fatalf("peers = %#v", data["peers"])
	}
}

func TestFileParsingErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		ext     string
		read    func(string) error
		want    string
	}{
		{name: "invalid JSON map", content: `{`, ext: ".json", read: func(path string) error { _, err := readMapFile(path); return err }, want: "decode JSON"},
		{name: "invalid YAML map", content: `value: [`, ext: ".yaml", read: func(path string) error { _, err := readMapFile(path); return err }, want: "decode YAML"},
		{name: "unsupported map extension", content: `value`, ext: ".txt", read: func(path string) error { _, err := readMapFile(path); return err }, want: "unsupported file extension"},
		{name: "empty data", content: `{}`, ext: ".json", read: func(path string) error { _, err := readDataFile(path); return err }, want: "must not be empty"},
		{name: "invalid object envelope", content: `{"kind":"Object"}`, ext: ".json", read: func(path string) error { _, err := readObjectFile(path); return err }, want: "expected apiVersion"},
		{name: "invalid object YAML", content: `spec: [`, ext: ".yaml", read: func(path string) error { _, err := readObjectFile(path); return err }, want: "decode YAML"},
		{name: "unsupported object extension", content: `value`, ext: ".txt", read: func(path string) error { _, err := readObjectFile(path); return err }, want: "unsupported file extension"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "input"+test.ext)
			writeTestFile(t, path, test.content)
			err := test.read(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestObjectDocumentValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		document map[string]any
		want     string
	}{
		{name: "spec", document: objectEnvelope("BuiltinTag", []any{map[string]any{"name": "x"}}), want: ""},
		{name: "missing spec", document: map[string]any{"apiVersion": "infrahub.app/v1", "kind": "Object"}, want: "spec must be an object"},
		{name: "missing kind", document: objectEnvelope("", []any{map[string]any{"name": "x"}}), want: "spec.kind must be"},
		{name: "expand range", document: map[string]any{"apiVersion": "infrahub.app/v1", "kind": "Object", "spec": map[string]any{"kind": "BuiltinTag", "parameters": map[string]any{"expand_range": true}, "data": []any{map[string]any{"name": "x"}}}}, want: "expand_range is not supported"},
		{name: "empty data", document: objectEnvelope("BuiltinTag", []any{}), want: "spec.data must be"},
		{name: "non-object data", document: objectEnvelope("BuiltinTag", []any{"invalid"}), want: "spec.data[0] must be an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseObjectDocument("input.yml", test.document)
			if test.want == "" {
				if err != nil || result.Kind != "BuiltinTag" {
					t.Fatalf("result=%#v err=%v", result, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestNoStructuredDocuments(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeTestFile(t, filepath.Join(directory, "README.txt"), "ignored")
	if _, err := readSchemaDocuments([]string{directory}); err == nil || !strings.Contains(err.Error(), "no schema documents") {
		t.Fatalf("schema error = %v", err)
	}
	if _, err := readObjectDocuments([]string{directory}); err == nil || !strings.Contains(err.Error(), "no object documents") {
		t.Fatalf("object error = %v", err)
	}
	if _, err := expandStructuredFiles([]string{filepath.Join(directory, "missing")}); err == nil {
		t.Fatal("expected missing path error")
	}
}

func objectEnvelope(kind string, data []any) map[string]any {
	return map[string]any{
		"apiVersion": "infrahub.app/v1",
		"kind":       "Object",
		"spec": map[string]any{
			"kind": kind,
			"data": data,
		},
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
