package cli

import "strings"

// filterSchemaKinds filters the schema kinds.
func filterSchemaKinds(schema any, needle string) []any {
	needle = strings.ToLower(needle)
	items := schemaCollections(schema)
	result := make([]any, 0, len(items))
	for _, item := range items {
		if kindName(item) == "" || strings.Contains(strings.ToLower(kindName(item)), needle) {
			result = append(result, item)
		}
	}
	return result
}

// findSchemaKind finds the schema kind.
func findSchemaKind(schema any, kind string) (any, bool) {
	for _, item := range schemaCollections(schema) {
		if kindName(item) == kind {
			return item, true
		}
	}
	return nil, false
}

// schemaCollections returns schema node and generic collections from a document.
func schemaCollections(schema any) []any {
	root, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	var result []any
	for _, key := range []string{"nodes", "generics"} {
		items, ok := root[key].([]any)
		if !ok {
			continue
		}
		result = append(result, items...)
	}
	return result
}

// kindName extracts a schema kind name from supported schema shapes.
func kindName(value any) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"kind", "name"} {
		if value, ok := item[key].(string); ok {
			return value
		}
	}
	return ""
}
