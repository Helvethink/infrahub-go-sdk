package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	"github.com/Helvethink/infrahub-go-sdk/pkg/telemetry"
	flag "github.com/spf13/pflag"
)

type dumpRecord struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	GraphQLJSON string `json:"graphql_json"`
}

func (r Runner) runDump(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	flags := flag.NewFlagSet("dump", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	directory := flags.String("directory", ".", "output directory")
	commandBranch := flags.String("branch", branch, "source branch")
	limit := flags.Int("limit", 100, "page size")
	var kinds multiFlag
	var namespaces multiFlag
	var excludes multiFlag
	flags.Var(&kinds, "kind", "node kind to export (repeatable)")
	flags.Var(&namespaces, "namespace", "namespace to export (repeatable)")
	flags.Var(&excludes, "exclude", "node kind to omit (repeatable)")
	if err := parseInterspersed(flags, args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		return r.usageError("usage: infrahubctl dump [--namespace NAME] [--kind KIND] [--directory DIR]")
	}
	if *limit <= 0 {
		return r.usageError("--limit must be positive")
	}
	if err := os.MkdirAll(*directory, 0o755); err != nil {
		return r.fail(fmt.Errorf("create dump directory: %w", err))
	}
	nodesPath, relationshipsPath := filepath.Join(*directory, "nodes.json"), filepath.Join(*directory, "relationships.json")
	for _, path := range []string{nodesPath, relationshipsPath} {
		if _, err := os.Stat(path); err == nil {
			return r.fail(fmt.Errorf("refusing to overwrite %s", path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return r.fail(err)
		}
	}
	var rawSchema map[string]any
	for _, namespace := range namespaces {
		if namespace == "Internal" || namespace == "Infrahub" || namespace == "Schema" {
			return r.usageError("namespace " + namespace + " cannot be dumped")
		}
	}
	if err := client.Schema.Fetch(ctx, *commandBranch, namespaces, &rawSchema); err != nil {
		return r.fail(err)
	}
	if len(kinds) == 0 {
		kinds = schemaNodeKinds(rawSchema, namespaces, excludes)
	}
	if len(kinds) == 0 {
		return r.fail(fmt.Errorf("branch schema does not contain exportable node kinds"))
	}
	selectionsByKind := make(map[string][]node.Selection, len(kinds))
	for _, kind := range kinds {
		selections, err := dumpSelections(rawSchema, kind)
		if err != nil {
			return r.fail(err)
		}
		selectionsByKind[kind] = selections
	}
	file, err := os.CreateTemp(*directory, ".nodes-*.tmp")
	if err != nil {
		return r.fail(err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	encoder := json.NewEncoder(file)
	count := 0
	for _, kind := range kinds {
		for offset := 0; ; offset += *limit {
			page, queryErr := client.Nodes.Query(ctx, kind, node.QueryOptions{Branch: *commandBranch, Offset: offset, Limit: *limit, Selections: selectionsByKind[kind]})
			if queryErr != nil {
				_ = file.Close()
				return r.fail(queryErr)
			}
			for _, item := range page.Nodes {
				payload, marshalErr := json.Marshal(item.Fields)
				if marshalErr != nil {
					_ = file.Close()
					return r.fail(marshalErr)
				}
				if err := encoder.Encode(dumpRecord{ID: item.ID, Kind: kind, GraphQLJSON: string(payload)}); err != nil {
					_ = file.Close()
					return r.fail(err)
				}
				count++
			}
			if len(page.Nodes) < *limit || offset+len(page.Nodes) >= page.Count {
				break
			}
		}
	}
	if err := file.Close(); err != nil {
		return r.fail(err)
	}
	manyRelationships := []any{}
	identifiers := manyRelationshipIdentifiers(rawSchema, kinds)
	if len(identifiers) > 0 {
		var response struct {
			Relationship struct {
				Edges []any `json:"edges"`
			} `json:"Relationship"`
		}
		err := client.Execute(ctx, infrahub.GraphQLRequest{
			Query:     `query GetRelationships($relationship_identifiers: [String!]!) { Relationship(ids: $relationship_identifiers) { edges { node { identifier peers { id kind } } } } }`,
			Variables: map[string]any{"relationship_identifiers": identifiers}, Branch: *commandBranch,
		}, &response)
		if err != nil {
			return r.fail(err)
		}
		manyRelationships = response.Relationship.Edges
	}
	if err := os.Rename(temporaryPath, nodesPath); err != nil {
		return r.fail(err)
	}
	relationships, err := os.OpenFile(relationshipsPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return r.fail(err)
	}
	if err := json.NewEncoder(relationships).Encode(manyRelationships); err != nil {
		_ = relationships.Close()
		return r.fail(err)
	}
	if err := relationships.Close(); err != nil {
		return r.fail(err)
	}
	return r.writeJSON(map[string]any{"nodes": count, "directory": *directory})
}

func manyRelationshipIdentifiers(schema map[string]any, kinds []string) []string {
	selected := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		selected[kind] = struct{}{}
	}
	seen := map[string]struct{}{}
	var result []string
	items, _ := schema["nodes"].([]any)
	for _, raw := range items {
		definition, _ := raw.(map[string]any)
		kind, _ := definition["kind"].(string)
		if kind == "" {
			namespace, _ := definition["namespace"].(string)
			name, _ := definition["name"].(string)
			kind = namespace + name
		}
		if _, ok := selected[kind]; !ok {
			continue
		}
		relationships, _ := definition["relationships"].([]any)
		for _, rawRelationship := range relationships {
			relationship, _ := rawRelationship.(map[string]any)
			cardinality, _ := relationship["cardinality"].(string)
			optional, _ := relationship["optional"].(bool)
			identifier, _ := relationship["identifier"].(string)
			peer, _ := relationship["peer"].(string)
			if cardinality != "many" || !optional || identifier == "" {
				continue
			}
			if _, ok := selected[peer]; !ok {
				continue
			}
			if _, ok := seen[identifier]; !ok {
				seen[identifier] = struct{}{}
				result = append(result, identifier)
			}
		}
	}
	sort.Strings(result)
	return result
}

func schemaNodeKinds(schema map[string]any, namespaces, excludes []string) []string {
	wantedNamespaces := make(map[string]struct{}, len(namespaces))
	for _, namespace := range namespaces {
		wantedNamespaces[namespace] = struct{}{}
	}
	excludedKinds := make(map[string]struct{}, len(excludes))
	for _, kind := range excludes {
		excludedKinds[kind] = struct{}{}
	}
	var result []string
	items, _ := schema["nodes"].([]any)
	for _, raw := range items {
		definition, _ := raw.(map[string]any)
		namespace, _ := definition["namespace"].(string)
		if namespace == "Internal" || namespace == "Infrahub" || namespace == "Schema" {
			continue
		}
		if len(wantedNamespaces) > 0 {
			if _, ok := wantedNamespaces[namespace]; !ok {
				continue
			}
		}
		kind, _ := definition["kind"].(string)
		if kind == "" {
			name, _ := definition["name"].(string)
			kind = namespace + name
		}
		if kind == "" {
			continue
		}
		if _, excluded := excludedKinds[kind]; !excluded {
			result = append(result, kind)
		}
	}
	sort.Strings(result)
	return result
}

func dumpSelections(schema map[string]any, kind string) ([]node.Selection, error) {
	for _, section := range []string{"generics", "nodes"} {
		items, _ := schema[section].([]any)
		for _, raw := range items {
			definition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			definitionKind, _ := definition["kind"].(string)
			if definitionKind == "" {
				namespace, _ := definition["namespace"].(string)
				name, _ := definition["name"].(string)
				definitionKind = namespace + name
			}
			if definitionKind != kind {
				continue
			}
			var selections []node.Selection
			if attributes, ok := definition["attributes"].([]any); ok {
				for _, rawAttribute := range attributes {
					attribute, _ := rawAttribute.(map[string]any)
					if name, ok := attribute["name"].(string); ok && name != "" {
						selections = append(selections, node.Select(name, node.Select("value")))
					}
				}
			}
			if relationships, ok := definition["relationships"].([]any); ok {
				for _, rawRelationship := range relationships {
					relationship, _ := rawRelationship.(map[string]any)
					name, _ := relationship["name"].(string)
					cardinality, _ := relationship["cardinality"].(string)
					if name == "" {
						continue
					}
					if cardinality == "many" {
						selections = append(selections, node.Select(name, node.Select("edges", node.Select("node", node.Select("id")))))
					} else {
						selections = append(selections, node.Select(name, node.Select("node", node.Select("id"))))
					}
				}
			}
			sort.Slice(selections, func(i, j int) bool { return selections[i].Name < selections[j].Name })
			return selections, nil
		}
	}
	return nil, fmt.Errorf("kind %q is not present in the branch schema", kind)
}

func (r Runner) runLoad(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	flags := flag.NewFlagSet("load", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	directory := flags.String("directory", ".", "dump directory")
	commandBranch := flags.String("branch", branch, "target branch")
	continueOnError := flags.Bool("continue-on-error", false, "continue after rejected nodes")
	if err := parseInterspersed(flags, args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		return r.usageError("usage: infrahubctl load [--directory DIR]")
	}
	file, err := os.Open(filepath.Join(*directory, "nodes.json"))
	if err != nil {
		return r.fail(err)
	}
	defer func() { _ = file.Close() }()
	relationshipData, err := os.ReadFile(filepath.Join(*directory, "relationships.json"))
	if err != nil {
		return r.fail(err)
	}
	var relationshipEdges []struct {
		Node struct {
			Identifier string `json:"identifier"`
			Peers      []struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			} `json:"peers"`
		} `json:"node"`
	}
	if err := json.Unmarshal(relationshipData, &relationshipEdges); err != nil {
		return r.fail(fmt.Errorf("decode relationships.json: %w", err))
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	loaded, failed := 0, 0
	for scanner.Scan() {
		var record dumpRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return r.fail(fmt.Errorf("decode nodes.json line %d: %w", loaded+failed+1, err))
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(record.GraphQLJSON), &data); err != nil {
			return r.fail(fmt.Errorf("decode node %q: %w", record.ID, err))
		}
		delete(data, "kind")
		delete(data, "display_label")
		delete(data, "__typename")
		if record.ID != "" {
			data["id"] = record.ID
		}
		if _, err := client.Nodes.Upsert(ctx, record.Kind, data, *commandBranch); err != nil {
			failed++
			if !*continueOnError {
				return r.fail(err)
			}
			continue
		}
		loaded++
	}
	if err := scanner.Err(); err != nil {
		return r.fail(err)
	}
	if len(relationshipEdges) > 0 {
		var rawSchema map[string]any
		if err := client.Schema.Fetch(ctx, *commandBranch, nil, &rawSchema); err != nil {
			return r.fail(err)
		}
		relationshipNames := relationshipNamesByKind(rawSchema)
		for _, edge := range relationshipEdges {
			if len(edge.Node.Peers) != 2 {
				failed++
				if !*continueOnError {
					return r.fail(fmt.Errorf("relationship %q must have exactly two peers", edge.Node.Identifier))
				}
				continue
			}
			source, destination := edge.Node.Peers[0], edge.Node.Peers[1]
			name := relationshipNames[source.Kind+"\x00"+edge.Node.Identifier]
			if name == "" {
				source, destination = destination, source
				name = relationshipNames[source.Kind+"\x00"+edge.Node.Identifier]
			}
			if name == "" {
				failed++
				if !*continueOnError {
					return r.fail(fmt.Errorf("relationship %q is absent from the branch schema", edge.Node.Identifier))
				}
				continue
			}
			_, err := client.Nodes.Upsert(ctx, source.Kind, map[string]any{
				"id": source.ID, name: []map[string]any{{"id": destination.ID}},
			}, *commandBranch)
			if err != nil {
				failed++
				if !*continueOnError {
					return r.fail(err)
				}
			}
		}
	}
	if code := r.writeJSON(map[string]int{"loaded": loaded, "failed": failed}); code != 0 {
		return code
	}
	if failed > 0 {
		return 1
	}
	return 0
}

func relationshipNamesByKind(schema map[string]any) map[string]string {
	result := map[string]string{}
	items, _ := schema["nodes"].([]any)
	for _, raw := range items {
		definition, _ := raw.(map[string]any)
		kind, _ := definition["kind"].(string)
		if kind == "" {
			namespace, _ := definition["namespace"].(string)
			name, _ := definition["name"].(string)
			kind = namespace + name
		}
		relationships, _ := definition["relationships"].([]any)
		for _, rawRelationship := range relationships {
			relationship, _ := rawRelationship.(map[string]any)
			identifier, _ := relationship["identifier"].(string)
			name, _ := relationship["name"].(string)
			if kind != "" && identifier != "" && name != "" {
				result[kind+"\x00"+identifier] = name
			}
		}
	}
	return result
}

type menuDocument struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Spec       struct {
		Kind string           `json:"kind" yaml:"kind"`
		Data []map[string]any `json:"data" yaml:"data"`
	} `json:"spec" yaml:"spec"`
}

func readMenu(path string) (menuDocument, error) {
	var result menuDocument
	document, err := readMapFile(path)
	if err != nil {
		return result, err
	}
	data, err := json.Marshal(document)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.APIVersion != "infrahub.app/v1" || result.Kind != "Menu" {
		return result, fmt.Errorf("%s: expected apiVersion infrahub.app/v1 and kind Menu", path)
	}
	if result.Spec.Kind == "" {
		result.Spec.Kind = "CoreMenuItem"
	}
	if len(result.Spec.Data) == 0 {
		return result, fmt.Errorf("%s: spec.data must be a non-empty list", path)
	}
	for index, item := range result.Spec.Data {
		if err := prepareMenuItem(item, index); err != nil {
			return result, fmt.Errorf("%s: spec.data[%d]: %w", path, index, err)
		}
	}
	return result, nil
}

func prepareMenuItem(item map[string]any, index int) error {
	for _, key := range []string{"namespace", "name", "label"} {
		if value, ok := item[key].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a non-empty string", key)
		}
	}
	if _, ok := item["order_weight"]; !ok {
		item["order_weight"] = (index + 1) * 1000
	}
	if _, ok := item["path"]; !ok {
		if kind, ok := item["kind"].(string); ok && kind != "" {
			item["path"] = "/objects/" + kind
		}
	}
	if children, ok := item["children"].([]any); ok {
		for childIndex, raw := range children {
			child, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("children[%d] must be an object", childIndex)
			}
			if err := prepareMenuItem(child, childIndex); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r Runner) runMenu(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) != 2 {
		return r.usageError("usage: infrahubctl menu <load|validate> <file>")
	}
	document, err := readMenu(args[1])
	if err != nil {
		return r.fail(err)
	}
	if args[0] == "validate" {
		return r.writeJSON(map[string]any{"valid": true, "items": len(document.Spec.Data)})
	}
	if args[0] != "load" {
		return r.usageError("infrahubctl: unknown menu command " + args[0])
	}
	for _, item := range document.Spec.Data {
		if _, err := client.Nodes.Upsert(ctx, document.Spec.Kind, normalizeObjectData(item), branch); err != nil {
			return r.fail(err)
		}
	}
	return r.writeJSON(map[string]int{"loaded": len(document.Spec.Data)})
}

func (r Runner) runTelemetry(ctx context.Context, client *infrahub.Client, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl telemetry <list|export>")
	}
	flags := flag.NewFlagSet("telemetry "+args[0], flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	start, end := flags.String("start", "", "start date (RFC3339)"), flags.String("end", "", "end date (RFC3339)")
	limit, offset := flags.Int("limit", 100, "maximum snapshots"), flags.Int("offset", 0, "snapshot offset")
	out := flags.String("out", "", "export file (default stdout)")
	if err := parseInterspersed(flags, args[1:]); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		return r.usageError("usage: infrahubctl telemetry " + args[0] + " [flags]")
	}
	options := telemetry.ListOptions{Limit: *limit, Offset: *offset}
	var err error
	if *start != "" {
		options.StartDate, err = time.Parse(time.RFC3339, *start)
		if err != nil {
			return r.usageError("--start must use RFC3339")
		}
	}
	if *end != "" {
		options.EndDate, err = time.Parse(time.RFC3339, *end)
		if err != nil {
			return r.usageError("--end must use RFC3339")
		}
	}
	var value any
	switch args[0] {
	case "export":
		value, err = client.Telemetry.All(ctx, options)
	case "list":
		value, err = client.Telemetry.List(ctx, options)
	default:
		return r.usageError("infrahubctl: unknown telemetry command " + args[0])
	}
	if err != nil {
		return r.fail(err)
	}
	if *out == "" {
		return r.writeJSON(value)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		data = append(data, '\n')
		err = os.WriteFile(*out, data, 0o600)
	}
	if err != nil {
		return r.fail(err)
	}
	return 0
}

func (r Runner) runValidate(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) < 2 {
		return r.usageError("usage: infrahubctl validate <schema|graphql-query> <file> [flags]")
	}
	if args[0] == "schema" {
		documents, err := readSchemaDocuments(args[1:])
		if err != nil {
			return r.fail(err)
		}
		for index, document := range documents {
			if len(document) == 0 {
				return r.fail(fmt.Errorf("schema document %d is empty", index))
			}
			if _, nodes := document["nodes"]; !nodes {
				if _, generics := document["generics"]; !generics {
					return r.fail(fmt.Errorf("schema document %d must define nodes or generics", index))
				}
			}
		}
		return r.writeJSON(map[string]any{"valid": true, "documents": len(documents)})
	}
	if args[0] != "graphql-query" {
		return r.usageError("infrahubctl: unknown validate command " + args[0])
	}
	flags := flag.NewFlagSet("validate graphql-query", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	commandBranch, out := flags.String("branch", branch, "target branch"), flags.String("out", "", "output file")
	var variables multiFlag
	flags.Var(&variables, "variable", "GraphQL variable key=value")
	if err := parseInterspersed(flags, args[1:]); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 1 {
		return r.usageError("usage: infrahubctl validate graphql-query [flags] <query-file>")
	}
	query, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return r.fail(err)
	}
	values, err := parseAssignments(variables, false)
	if err != nil {
		return r.usageError(err.Error())
	}
	var result any
	if err := client.Execute(ctx, infrahub.GraphQLRequest{Query: string(query), Variables: values, Branch: *commandBranch}, &result); err != nil {
		return r.fail(err)
	}
	if *out == "" {
		return r.writeJSON(result)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err == nil {
		data = append(data, '\n')
		err = os.WriteFile(*out, data, 0o600)
	}
	if err != nil {
		return r.fail(err)
	}
	return 0
}

func (r Runner) runProtocols(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	flags := flag.NewFlagSet("protocols", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	out := flags.String("out", "schema_protocols.py", "generated Python file")
	commandBranch := flags.String("branch", branch, "source branch")
	syncMode := flags.Bool("sync", false, "generate synchronous relationship types")
	var schemaPaths multiFlag
	flags.Var(&schemaPaths, "schema", "local schema file or directory (repeatable)")
	if err := parseInterspersed(flags, args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 {
		return r.usageError("usage: infrahubctl protocols [--out FILE]")
	}
	var schema map[string]any
	if len(schemaPaths) > 0 {
		documents, err := readSchemaDocuments(schemaPaths)
		if err != nil {
			return r.fail(err)
		}
		schema = mergeSchemaDocuments(documents)
	} else {
		if client == nil {
			return r.usageError("protocols requires --schema or an Infrahub address")
		}
		if err := client.Schema.Fetch(ctx, *commandBranch, nil, &schema); err != nil {
			return r.fail(err)
		}
	}
	source, err := generateProtocols(schema, *syncMode)
	if err != nil {
		return r.fail(err)
	}
	if *out == "-" {
		_, err = io.WriteString(r.Stdout, source)
	} else {
		err = os.WriteFile(*out, []byte(source), 0o600)
	}
	if err != nil {
		return r.fail(err)
	}
	return 0
}

func mergeSchemaDocuments(documents []map[string]any) map[string]any {
	merged := map[string]any{"nodes": []any{}, "generics": []any{}}
	for _, document := range documents {
		for _, section := range []string{"nodes", "generics"} {
			if items, ok := document[section].([]any); ok {
				merged[section] = append(merged[section].([]any), items...)
			}
		}
	}
	return merged
}

func generateProtocols(schema map[string]any, syncMode bool) (string, error) {
	type field struct {
		Name string
		Type string
	}
	type definition struct {
		Kind   string
		Fields []field
	}
	var definitions []definition
	for _, section := range []string{"generics", "nodes"} {
		items, _ := schema[section].([]any)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			kind, _ := item["kind"].(string)
			if kind == "" {
				namespace, _ := item["namespace"].(string)
				name, _ := item["name"].(string)
				kind = namespace + name
			}
			if kind == "" {
				continue
			}
			definition := definition{Kind: kind}
			if attrs, ok := item["attributes"].([]any); ok {
				for _, attrRaw := range attrs {
					if attr, ok := attrRaw.(map[string]any); ok {
						if name, ok := attr["name"].(string); ok {
							definition.Fields = append(definition.Fields, field{Name: name, Type: protocolAttributeType(attr)})
						}
					}
				}
			}
			if relationships, ok := item["relationships"].([]any); ok {
				for _, relationshipRaw := range relationships {
					relationship, _ := relationshipRaw.(map[string]any)
					name, _ := relationship["name"].(string)
					peer, _ := relationship["peer"].(string)
					cardinality, _ := relationship["cardinality"].(string)
					if name == "" || peer == "" {
						continue
					}
					fieldType := peer
					if cardinality == "many" {
						fieldType = "Sequence[" + peer + "]"
					}
					if optional, _ := relationship["optional"].(bool); optional {
						fieldType += " | None"
					}
					definition.Fields = append(definition.Fields, field{Name: name, Type: fieldType})
				}
			}
			sort.Slice(definition.Fields, func(i, j int) bool { return definition.Fields[i].Name < definition.Fields[j].Name })
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) == 0 {
		return "", fmt.Errorf("schema does not contain node or generic definitions")
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Kind < definitions[j].Kind })
	var output strings.Builder
	output.WriteString("# Generated by infrahubctl protocols. DO NOT EDIT.\nfrom __future__ import annotations\nfrom datetime import datetime\nfrom typing import Any, Protocol, Sequence\n\n")
	if syncMode {
		output.WriteString("# Synchronous SDK mode requested.\n\n")
	}
	for _, definition := range definitions {
		output.WriteString("class " + definition.Kind + "(Protocol):\n    id: str\n")
		for _, field := range definition.Fields {
			output.WriteString("    " + field.Name + ": " + field.Type + "\n")
		}
		output.WriteString("\n")
	}
	return output.String(), nil
}

func protocolAttributeType(attribute map[string]any) string {
	kind, _ := attribute["kind"].(string)
	types := map[string]string{
		"Boolean": "bool", "Checkbox": "bool", "DateTime": "datetime",
		"Number": "int", "Bandwidth": "int", "NumberPool": "int",
		"List": "list[Any]", "JSON": "Any",
	}
	fieldType := types[kind]
	if fieldType == "" {
		fieldType = "str"
	}
	if optional, _ := attribute["optional"].(bool); optional {
		fieldType += " | None"
	}
	return fieldType
}

func (r Runner) runMarketplace(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl marketplace <list|search|show|get>")
	}
	flags := flag.NewFlagSet("marketplace "+args[0], flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	base := flags.String("url", "https://marketplace.infrahub.app", "marketplace base URL")
	collection := flags.Bool("collection", false, "operate on a collection")
	query := flags.String("query", "", "search text")
	out := flags.String("out", "", "download destination")
	limit := flags.Int("limit", 100, "result limit")
	if err := parseInterspersed(flags, args[1:]); err != nil {
		return flagExitCode(err)
	}
	parsed, err := url.Parse(*base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return r.usageError("--url must be an absolute HTTP(S) URL")
	}
	path := "/api/v1/schemas"
	if *collection {
		path = "/api/v1/collections"
	}
	values := url.Values{}
	switch args[0] {
	case "list":
		values.Set("limit", strconv.Itoa(*limit))
		if flags.NArg() != 0 {
			return r.usageError("usage: infrahubctl marketplace list [flags]")
		}
	case "search":
		if *query == "" && flags.NArg() == 1 {
			*query = flags.Arg(0)
		}
		if *query == "" {
			return r.usageError("marketplace search requires a query")
		}
		values.Set("search", *query)
		values.Set("limit", strconv.Itoa(*limit))
	case "show", "get":
		if flags.NArg() != 1 {
			return r.usageError("marketplace " + args[0] + " requires namespace/name")
		}
		parts := strings.Split(flags.Arg(0), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return r.usageError("marketplace identifier must use namespace/name")
		}
		path += "/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
		if args[0] == "get" && !*collection {
			path += "/download"
		}
	default:
		return r.usageError("infrahubctl: unknown marketplace command " + args[0])
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return r.fail(err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "infrahubctl")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return r.fail(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		return r.fail(err)
	}
	if len(body) > 16<<20 {
		return r.fail(fmt.Errorf("marketplace response exceeds 16 MiB"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return r.fail(fmt.Errorf("marketplace returned HTTP %d", response.StatusCode))
	}
	if args[0] == "get" && *out != "" {
		if err := os.WriteFile(*out, body, 0o600); err != nil {
			return r.fail(err)
		}
		return 0
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		body = append(pretty.Bytes(), '\n')
	}
	if _, err := r.Stdout.Write(body); err != nil {
		return r.fail(err)
	}
	return 0
}
