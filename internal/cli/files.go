package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// readSchemaDocuments reads the schema documents.
func readSchemaDocuments(paths []string) ([]map[string]any, error) {
	files, err := expandStructuredFiles(paths)
	if err != nil {
		return nil, err
	}
	documents := make([]map[string]any, 0, len(files))
	for _, path := range files {
		document, err := readMapFile(path)
		if err != nil {
			return nil, err
		}
		if len(document) == 0 {
			continue
		}
		documents = append(documents, document)
	}
	if len(documents) == 0 {
		return nil, fmt.Errorf("no schema documents found")
	}
	return documents, nil
}

// readDataFile reads the data file.
func readDataFile(path string) (map[string]any, error) {
	document, err := readMapFile(path)
	if err != nil {
		return nil, err
	}
	if len(document) == 0 {
		return nil, fmt.Errorf("%s: document must not be empty", path)
	}
	return document, nil
}

// objectDocument holds internal data used by the object document workflow.
type objectDocument struct {
	Kind string
	Data []map[string]any
}

// readObjectDocuments reads the object documents.
func readObjectDocuments(paths []string) ([]objectDocument, error) {
	files, err := expandStructuredFiles(paths)
	if err != nil {
		return nil, err
	}
	var documents []objectDocument
	for _, path := range files {
		items, err := readObjectFile(path)
		if err != nil {
			return nil, err
		}
		documents = append(documents, items...)
	}
	if len(documents) == 0 {
		return nil, fmt.Errorf("no object documents found")
	}
	return documents, nil
}

// readObjectFile reads the object file.
func readObjectFile(path string) ([]objectDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return readJSONObjectFile(path, data)
	case ".yaml", ".yml":
		return readYAMLObjectFile(path, data)
	default:
		return nil, fmt.Errorf("%s: unsupported file extension", path)
	}
}

// readJSONObjectFile reads the JSON object file.
func readJSONObjectFile(path string, data []byte) ([]objectDocument, error) {
	var document map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%s: decode JSON: %w", path, err)
	}
	item, err := parseObjectDocument(path, document)
	if err != nil {
		return nil, err
	}
	return []objectDocument{item}, nil
}

// readYAMLObjectFile reads the yaml object file.
func readYAMLObjectFile(path string, data []byte) ([]objectDocument, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var result []objectDocument
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: decode YAML: %w", path, err)
		}
		if len(document) == 0 {
			continue
		}
		item, err := parseObjectDocument(path, document)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
}

// parseObjectDocument parses the object document.
func parseObjectDocument(path string, document map[string]any) (objectDocument, error) {
	if document["apiVersion"] != "infrahub.app/v1" || document["kind"] != "Object" {
		return objectDocument{}, fmt.Errorf("%s: expected apiVersion infrahub.app/v1 and kind Object", path)
	}
	spec, ok := document["spec"].(map[string]any)
	if !ok {
		return objectDocument{}, fmt.Errorf("%s: spec must be an object", path)
	}
	kind, ok := spec["kind"].(string)
	if !ok || kind == "" {
		return objectDocument{}, fmt.Errorf("%s: spec.kind must be a non-empty string", path)
	}
	if parameters, ok := spec["parameters"].(map[string]any); ok {
		if expandRange, _ := parameters["expand_range"].(bool); expandRange {
			return objectDocument{}, fmt.Errorf("%s: parameters.expand_range is not supported by the Go CLI yet", path)
		}
	}
	return objectDocumentFromData(path, kind, spec["data"])
}

// objectDocumentFromData extracts a kind and mutation data from a decoded document.
func objectDocumentFromData(path, kind string, value any) (objectDocument, error) {
	rawData, ok := value.([]any)
	if !ok || len(rawData) == 0 {
		return objectDocument{}, fmt.Errorf("%s: spec.data must be a non-empty list", path)
	}
	data := make([]map[string]any, 0, len(rawData))
	for index, item := range rawData {
		values, ok := item.(map[string]any)
		if !ok {
			return objectDocument{}, fmt.Errorf("%s: spec.data[%d] must be an object", path, index)
		}
		data = append(data, values)
	}
	return objectDocument{Kind: kind, Data: data}, nil
}

// normalizeObjectData normalizes the object data.
func normalizeObjectData(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		switch key {
		case "id", "hfid", "children":
			output[key] = value
		default:
			output[key] = normalizeObjectValue(value)
		}
	}
	return output
}

// normalizeObjectValue normalizes the object value.
func normalizeObjectValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case []any:
		return typed
	default:
		return map[string]any{"value": typed}
	}
}

// expandStructuredFiles expands the structured files.
func expandStructuredFiles(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			result = append(result, path)
			continue
		}
		err = filepath.WalkDir(path, collectStructuredFile(&result))
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// collectStructuredFile collects the structured file.
func collectStructuredFile(result *[]string) fs.WalkDirFunc {
	return func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && isStructuredFile(path) {
			*result = append(*result, path)
		}
		return nil
	}
}

// isStructuredFile reports whether path has a supported JSON or YAML extension.
func isStructuredFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

// readMapFile reads the map file.
func readMapFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil {
			return nil, fmt.Errorf("%s: decode JSON: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("%s: decode YAML: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported file extension", path)
	}
	return result, nil
}
