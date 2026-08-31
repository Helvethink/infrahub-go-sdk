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

	flag "github.com/spf13/pflag"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	"github.com/Helvethink/infrahub-go-sdk/pkg/telemetry"
)

// dumpRecord holds internal data used by the dump record workflow.
type dumpRecord struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	GraphQLJSON string `json:"graphql_json"`
}

// dumpOptions holds internal data used by the dump options workflow.
type dumpOptions struct {
	directory  string
	branch     string
	limit      int
	kinds      []string
	namespaces []string
	excludes   []string
}

// dumpPaths holds internal data used by the dump paths workflow.
type dumpPaths struct {
	nodes         string
	relationships string
}

// dumpPlan holds internal data used by the dump plan workflow.
type dumpPlan struct {
	schema           map[string]any
	kinds            []string
	selectionsByKind map[string][]node.Selection
}

// runDump runs the dump.
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
	return r.executeDump(ctx, client, dumpOptions{
		directory:  *directory,
		branch:     *commandBranch,
		limit:      *limit,
		kinds:      kinds,
		namespaces: namespaces,
		excludes:   excludes,
	})
}

// executeDump executes the dump.
func (r Runner) executeDump(ctx context.Context, client *infrahub.Client, options dumpOptions) int {
	paths, err := prepareDumpPaths(options.directory)
	if err != nil {
		return r.fail(err)
	}
	if err := validateDumpNamespaces(options.namespaces); err != nil {
		return r.usageError(err.Error())
	}
	plan, err := prepareDumpPlan(ctx, client, options)
	if err != nil {
		return r.fail(err)
	}
	return r.exportDump(ctx, client, options, paths, plan)
}

// prepareDumpPaths prepares the dump paths.
func prepareDumpPaths(directory string) (dumpPaths, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return dumpPaths{}, fmt.Errorf("create dump directory: %w", err)
	}
	paths := dumpPaths{
		nodes:         filepath.Join(directory, "nodes.json"),
		relationships: filepath.Join(directory, "relationships.json"),
	}
	for _, path := range []string{paths.nodes, paths.relationships} {
		if _, err := os.Stat(path); err == nil {
			return dumpPaths{}, fmt.Errorf("refusing to overwrite %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return dumpPaths{}, err
		}
	}
	return paths, nil
}

// validateDumpNamespaces validates the dump namespaces.
func validateDumpNamespaces(namespaces []string) error {
	for _, namespace := range namespaces {
		if namespace == "Internal" || namespace == "Infrahub" || namespace == "Schema" {
			return fmt.Errorf("namespace %s cannot be dumped", namespace)
		}
	}
	return nil
}

// prepareDumpPlan prepares the dump plan.
func prepareDumpPlan(ctx context.Context, client *infrahub.Client, options dumpOptions) (dumpPlan, error) {
	var rawSchema map[string]any
	if err := client.Schema.Fetch(ctx, options.branch, options.namespaces, &rawSchema); err != nil {
		return dumpPlan{}, err
	}
	kinds := options.kinds
	if len(kinds) == 0 {
		kinds = schemaNodeKinds(rawSchema, options.namespaces, options.excludes)
	}
	if len(kinds) == 0 {
		return dumpPlan{}, fmt.Errorf("branch schema does not contain exportable node kinds")
	}
	selectionsByKind := make(map[string][]node.Selection, len(kinds))
	for _, kind := range kinds {
		selections, err := dumpSelections(rawSchema, kind)
		if err != nil {
			return dumpPlan{}, err
		}
		selectionsByKind[kind] = selections
	}
	return dumpPlan{schema: rawSchema, kinds: kinds, selectionsByKind: selectionsByKind}, nil
}

// exportDump writes node and relationship dump files from the prepared plan.
func (r Runner) exportDump(
	ctx context.Context,
	client *infrahub.Client,
	options dumpOptions,
	paths dumpPaths,
	plan dumpPlan,
) int {
	temporaryPath, count, err := writeDumpNodes(ctx, client, options, plan)
	if err != nil {
		return r.fail(err)
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	manyRelationships, err := fetchDumpRelationships(ctx, client, options.branch, plan.schema, plan.kinds)
	if err != nil {
		return r.fail(err)
	}
	if err := os.Rename(temporaryPath, paths.nodes); err != nil {
		return r.fail(err)
	}
	if err := writeDumpRelationships(paths.relationships, manyRelationships); err != nil {
		return r.fail(err)
	}
	return r.writeJSON(map[string]any{"nodes": count, "directory": options.directory})
}

// writeDumpNodes writes the dump nodes.
func writeDumpNodes(
	ctx context.Context,
	client *infrahub.Client,
	options dumpOptions,
	plan dumpPlan,
) (string, int, error) {
	file, err := os.CreateTemp(options.directory, ".nodes-*.tmp")
	if err != nil {
		return "", 0, err
	}
	temporaryPath := file.Name()
	count, err := encodeDumpNodes(ctx, client, json.NewEncoder(file), options, plan)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
		return "", 0, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", 0, err
	}
	return temporaryPath, count, nil
}

// encodeDumpNodes encodes the dump nodes.
func encodeDumpNodes(
	ctx context.Context,
	client *infrahub.Client,
	encoder *json.Encoder,
	options dumpOptions,
	plan dumpPlan,
) (int, error) {
	count := 0
	for _, kind := range plan.kinds {
		encoded, err := encodeDumpKind(ctx, client, encoder, options, kind, plan.selectionsByKind[kind])
		if err != nil {
			return 0, err
		}
		count += encoded
	}
	return count, nil
}

// encodeDumpKind encodes the dump kind.
func encodeDumpKind(
	ctx context.Context,
	client *infrahub.Client,
	encoder *json.Encoder,
	options dumpOptions,
	kind string,
	selections []node.Selection,
) (int, error) {
	count := 0
	for offset := 0; ; offset += options.limit {
		page, err := client.Nodes.Query(ctx, kind, node.QueryOptions{
			Branch: options.branch, Offset: offset, Limit: options.limit, Selections: selections,
		})
		if err != nil {
			return 0, err
		}
		encoded, err := encodeDumpPage(encoder, kind, page.Nodes)
		if err != nil {
			return 0, err
		}
		count += encoded
		if len(page.Nodes) < options.limit || offset+len(page.Nodes) >= page.Count {
			return count, nil
		}
	}
}

// encodeDumpPage encodes the dump page.
func encodeDumpPage(encoder *json.Encoder, kind string, items []node.Node) (int, error) {
	for _, item := range items {
		payload, err := json.Marshal(item.Fields)
		if err != nil {
			return 0, err
		}
		if err := encoder.Encode(dumpRecord{ID: item.ID, Kind: kind, GraphQLJSON: string(payload)}); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

// fetchDumpRelationships fetches the dump relationships.
func fetchDumpRelationships(
	ctx context.Context,
	client *infrahub.Client,
	branch string,
	schema map[string]any,
	kinds []string,
) ([]any, error) {
	identifiers := manyRelationshipIdentifiers(schema, kinds)
	if len(identifiers) == 0 {
		return []any{}, nil
	}
	var response struct {
		Relationship struct {
			Edges []any `json:"edges"`
		} `json:"Relationship"`
	}
	err := client.Execute(ctx, infrahub.GraphQLRequest{
		Query:     `query GetRelationships($relationship_identifiers: [String!]!) { Relationship(ids: $relationship_identifiers) { edges { node { identifier peers { id kind } } } } }`,
		Variables: map[string]any{"relationship_identifiers": identifiers}, Branch: branch,
	}, &response)
	return response.Relationship.Edges, err
}

// writeDumpRelationships writes the dump relationships.
func writeDumpRelationships(path string, values []any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(values); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// manyRelationshipIdentifiers extracts peer identifiers from cardinality-many edges.
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
		if _, ok := selected[schemaDefinitionKind(definition)]; !ok {
			continue
		}
		result = appendManyRelationshipIdentifiers(result, definition, selected, seen)
	}
	sort.Strings(result)
	return result
}

// schemaDefinitionKind returns the kind name declared by a schema definition.
func schemaDefinitionKind(definition map[string]any) string {
	if kind, _ := definition["kind"].(string); kind != "" {
		return kind
	}
	namespace, _ := definition["namespace"].(string)
	name, _ := definition["name"].(string)
	return namespace + name
}

// appendManyRelationshipIdentifiers appends the many relationship IDentifiers.
func appendManyRelationshipIdentifiers(
	result []string,
	definition map[string]any,
	selected map[string]struct{},
	seen map[string]struct{},
) []string {
	relationships, _ := definition["relationships"].([]any)
	for _, raw := range relationships {
		relationship, _ := raw.(map[string]any)
		identifier, ok := eligibleManyRelationshipIdentifier(relationship, selected)
		if !ok {
			continue
		}
		if _, duplicate := seen[identifier]; duplicate {
			continue
		}
		seen[identifier] = struct{}{}
		result = append(result, identifier)
	}
	return result
}

// eligibleManyRelationshipIdentifier filters peers that can be represented in a dump.
func eligibleManyRelationshipIdentifier(relationship map[string]any, selected map[string]struct{}) (string, bool) {
	cardinality, _ := relationship["cardinality"].(string)
	optional, _ := relationship["optional"].(bool)
	identifier, _ := relationship["identifier"].(string)
	if cardinality != "many" || !optional || identifier == "" {
		return "", false
	}
	peer, _ := relationship["peer"].(string)
	_, selectedPeer := selected[peer]
	return identifier, selectedPeer
}

// schemaNodeKinds selects dumpable node kinds from the schema response.
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

// dumpSelections builds query selections for one dumped kind.
func dumpSelections(schema map[string]any, kind string) ([]node.Selection, error) {
	definition, ok := findDumpSchemaDefinition(schema, kind)
	if !ok {
		return nil, fmt.Errorf("kind %q is not present in the branch schema", kind)
	}
	selections := dumpAttributeSelections(definition)
	selections = append(selections, dumpRelationshipSelections(definition)...)
	sort.Slice(selections, func(i, j int) bool { return selections[i].Name < selections[j].Name })
	return selections, nil
}

// findDumpSchemaDefinition finds the dump schema definition.
func findDumpSchemaDefinition(schema map[string]any, kind string) (map[string]any, bool) {
	for _, section := range []string{"generics", "nodes"} {
		items, _ := schema[section].([]any)
		for _, raw := range items {
			definition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if schemaDefinitionKind(definition) == kind {
				return definition, true
			}
		}
	}
	return nil, false
}

// dumpAttributeSelections builds selections for schema-defined attributes.
func dumpAttributeSelections(definition map[string]any) []node.Selection {
	attributes, _ := definition["attributes"].([]any)
	selections := make([]node.Selection, 0, len(attributes))
	for _, raw := range attributes {
		attribute, _ := raw.(map[string]any)
		name, _ := attribute["name"].(string)
		if name != "" {
			selections = append(selections, node.Select(name, node.Select("value")))
		}
	}
	return selections
}

// dumpRelationshipSelections builds selections for schema-defined relationships.
func dumpRelationshipSelections(definition map[string]any) []node.Selection {
	relationships, _ := definition["relationships"].([]any)
	selections := make([]node.Selection, 0, len(relationships))
	for _, raw := range relationships {
		relationship, _ := raw.(map[string]any)
		selection, ok := dumpRelationshipSelection(relationship)
		if ok {
			selections = append(selections, selection)
		}
	}
	return selections
}

// dumpRelationshipSelection builds the nested selection for one relationship.
func dumpRelationshipSelection(relationship map[string]any) (node.Selection, bool) {
	name, _ := relationship["name"].(string)
	if name == "" {
		return node.Selection{}, false
	}
	cardinality, _ := relationship["cardinality"].(string)
	if cardinality == "many" {
		return node.Select(name, node.Select("edges", node.Select("node", node.Select("id")))), true
	}
	return node.Select(name, node.Select("node", node.Select("id"))), true
}

// loadOptions holds internal data used by the load options workflow.
type loadOptions struct {
	directory       string
	branch          string
	continueOnError bool
}

// pendingDumpNode holds internal data used by the pending dump node workflow.
type pendingDumpNode struct {
	record        dumpRecord
	data          map[string]any
	relationships map[string]any
	hfid          []string
}

// dumpRelationshipPeer holds internal data used by the dump relationship peer workflow.
type dumpRelationshipPeer struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// dumpRelationshipEdge holds internal data used by the dump relationship edge workflow.
type dumpRelationshipEdge struct {
	Node struct {
		Identifier string                 `json:"identifier"`
		Peers      []dumpRelationshipPeer `json:"peers"`
	} `json:"node"`
}

// loadDumpData holds internal data used by the load dump data workflow.
type loadDumpData struct {
	nodes             []pendingDumpNode
	relationshipEdges []dumpRelationshipEdge
}

// loadProgress holds internal data used by the load progress workflow.
type loadProgress struct {
	loaded int
	failed int
	idMap  map[string]string
}

// runLoad runs the load.
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
	return r.executeLoad(ctx, client, loadOptions{
		directory: *directory, branch: *commandBranch, continueOnError: *continueOnError,
	})
}

// executeLoad executes the load.
func (r Runner) executeLoad(ctx context.Context, client *infrahub.Client, options loadOptions) int {
	dump, err := readLoadDump(options.directory)
	if err != nil {
		return r.fail(err)
	}
	progress, err := restoreDumpNodes(ctx, client.Nodes, dump.nodes, options)
	if err != nil {
		return r.fail(err)
	}
	failed, err := restoreDumpNodeRelationships(ctx, client.Nodes, dump.nodes, progress.idMap, options)
	progress.failed += failed
	if err != nil {
		return r.fail(err)
	}
	failed, err = restoreDumpRelationshipEdges(ctx, client, dump.relationshipEdges, progress.idMap, options)
	progress.failed += failed
	if err != nil {
		return r.fail(err)
	}
	return r.writeLoadResult(progress)
}

// readLoadDump reads the load dump.
func readLoadDump(directory string) (loadDumpData, error) {
	file, err := os.Open(filepath.Join(directory, "nodes.json"))
	if err != nil {
		return loadDumpData{}, err
	}
	defer func() { _ = file.Close() }()
	relationshipData, err := os.ReadFile(filepath.Join(directory, "relationships.json"))
	if err != nil {
		return loadDumpData{}, err
	}
	var relationshipEdges []dumpRelationshipEdge
	if err := json.Unmarshal(relationshipData, &relationshipEdges); err != nil {
		return loadDumpData{}, fmt.Errorf("decode relationships.json: %w", err)
	}
	nodes, err := readPendingDumpNodes(file)
	if err != nil {
		return loadDumpData{}, err
	}
	return loadDumpData{nodes: nodes, relationshipEdges: relationshipEdges}, nil
}

// readPendingDumpNodes reads the pending dump nodes.
func readPendingDumpNodes(input io.Reader) ([]pendingDumpNode, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 16<<20)
	var pending []pendingDumpNode
	for scanner.Scan() {
		item, err := decodePendingDumpNode(scanner.Bytes(), len(pending)+1)
		if err != nil {
			return nil, err
		}
		pending = append(pending, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pending, nil
}

// decodePendingDumpNode decodes the pending dump node.
func decodePendingDumpNode(data []byte, line int) (pendingDumpNode, error) {
	var record dumpRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return pendingDumpNode{}, fmt.Errorf("decode nodes.json line %d: %w", line, err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(record.GraphQLJSON), &fields); err != nil {
		return pendingDumpNode{}, fmt.Errorf("decode node %q: %w", record.ID, err)
	}
	fields, relationships, hfid := dumpMutationData(fields)
	return pendingDumpNode{record: record, data: fields, relationships: relationships, hfid: hfid}, nil
}

// restoreDumpNodes restores the dump nodes.
func restoreDumpNodes(
	ctx context.Context,
	service *node.Service,
	pending []pendingDumpNode,
	options loadOptions,
) (loadProgress, error) {
	progress := loadProgress{idMap: make(map[string]string, len(pending))}
	for index := range pending {
		item := &pending[index]
		restored, err := restoreDumpNode(ctx, service, item.record, item.data, item.hfid, options.branch)
		if err != nil {
			progress.failed++
			if !options.continueOnError {
				return progress, err
			}
			continue
		}
		if item.record.ID != "" {
			progress.idMap[item.record.ID] = restored.ID
		}
		progress.loaded++
	}
	return progress, nil
}

// restoreDumpNodeRelationships restores the dump node relationships.
func restoreDumpNodeRelationships(
	ctx context.Context,
	service *node.Service,
	pending []pendingDumpNode,
	idMap map[string]string,
	options loadOptions,
) (int, error) {
	failed := 0
	for _, item := range pending {
		sourceID := idMap[item.record.ID]
		if sourceID == "" || len(item.relationships) == 0 {
			continue
		}
		data := map[string]any{"id": sourceID}
		for name, value := range item.relationships {
			data[name] = remapRelatedNodeIDs(value, idMap)
		}
		if _, err := service.Upsert(ctx, item.record.Kind, data, options.branch); err != nil {
			failed++
			if !options.continueOnError {
				return failed, err
			}
		}
	}
	return failed, nil
}

// restoreDumpRelationshipEdges restores the dump relationship edges.
func restoreDumpRelationshipEdges(
	ctx context.Context,
	client *infrahub.Client,
	edges []dumpRelationshipEdge,
	idMap map[string]string,
	options loadOptions,
) (int, error) {
	if len(edges) == 0 {
		return 0, nil
	}
	var rawSchema map[string]any
	if err := client.Schema.Fetch(ctx, options.branch, nil, &rawSchema); err != nil {
		return 0, err
	}
	relationshipNames := relationshipNamesByKind(rawSchema)
	failed := 0
	for _, edge := range edges {
		source, destination, name, err := resolveDumpRelationship(edge, relationshipNames, idMap)
		if err != nil {
			failed++
			if !options.continueOnError {
				return failed, err
			}
			continue
		}
		_, err = client.Nodes.Upsert(ctx, source.Kind, map[string]any{
			"id": source.ID, name: []map[string]any{{"id": destination.ID}},
		}, options.branch)
		if err != nil {
			failed++
			if !options.continueOnError {
				return failed, err
			}
		}
	}
	return failed, nil
}

// resolveDumpRelationship resolves the dump relationship.
func resolveDumpRelationship(
	edge dumpRelationshipEdge,
	names map[string]string,
	idMap map[string]string,
) (dumpRelationshipPeer, dumpRelationshipPeer, string, error) {
	if len(edge.Node.Peers) != 2 {
		return dumpRelationshipPeer{}, dumpRelationshipPeer{}, "", fmt.Errorf(
			"relationship %q must have exactly two peers", edge.Node.Identifier,
		)
	}
	source, destination := edge.Node.Peers[0], edge.Node.Peers[1]
	source.ID = remapNodeID(source.ID, idMap)
	destination.ID = remapNodeID(destination.ID, idMap)
	name := names[source.Kind+"\x00"+edge.Node.Identifier]
	if name == "" {
		source, destination = destination, source
		name = names[source.Kind+"\x00"+edge.Node.Identifier]
	}
	if name == "" {
		return dumpRelationshipPeer{}, dumpRelationshipPeer{}, "", fmt.Errorf(
			"relationship %q is absent from the branch schema", edge.Node.Identifier,
		)
	}
	return source, destination, name, nil
}

// writeLoadResult writes the load result.
func (r Runner) writeLoadResult(progress loadProgress) int {
	if code := r.writeJSON(map[string]int{"loaded": progress.loaded, "failed": progress.failed}); code != 0 {
		return code
	}
	if progress.failed > 0 {
		return 1
	}
	return 0
}

// dumpMutationData converts serialized GraphQL data into node mutation input.
func dumpMutationData(data map[string]any) (map[string]any, map[string]any, []string) {
	hfid := stringSlice(data["hfid"])
	delete(data, "id")
	delete(data, "kind")
	delete(data, "hfid")
	delete(data, "display_label")
	delete(data, "__typename")
	relationships := map[string]any{}
	for name, value := range data {
		relationship, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if edges, ok := relationship["edges"].([]any); ok {
			relationships[name] = relatedNodeInputs(edges)
			delete(data, name)
			continue
		}
		if related, exists := relationship["node"]; exists {
			relationships[name] = relatedNodeInput(related)
			delete(data, name)
		}
	}
	return data, relationships, hfid
}

// stringSlice converts a dynamically decoded value to a string slice.
func stringSlice(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

// relatedNodeInputs normalizes relationship data into mutation inputs.
func relatedNodeInputs(edges []any) []map[string]any {
	result := make([]map[string]any, 0, len(edges))
	for _, rawEdge := range edges {
		edge, _ := rawEdge.(map[string]any)
		if related := relatedNodeInput(edge["node"]); related != nil {
			result = append(result, related)
		}
	}
	return result
}

// relatedNodeInput normalizes one relationship peer into mutation input.
func relatedNodeInput(value any) map[string]any {
	related, _ := value.(map[string]any)
	id, _ := related["id"].(string)
	if id == "" {
		return nil
	}
	return map[string]any{"id": id}
}

// remapRelatedNodeIDs replaces source node identifiers with their restored identifiers.
func remapRelatedNodeIDs(value any, ids map[string]string) any {
	switch related := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(related))
		for name, field := range related {
			result[name] = field
		}
		if id, _ := result["id"].(string); id != "" {
			result["id"] = remapNodeID(id, ids)
		}
		return result
	case []map[string]any:
		result := make([]map[string]any, 0, len(related))
		for _, item := range related {
			mapped, _ := remapRelatedNodeIDs(item, ids).(map[string]any)
			result = append(result, mapped)
		}
		return result
	default:
		return value
	}
}

// remapNodeID remaps the node ID.
func remapNodeID(id string, ids map[string]string) string {
	if replacement := ids[id]; replacement != "" {
		return replacement
	}
	return id
}

// restoreDumpNode restores the dump node.
func restoreDumpNode(
	ctx context.Context,
	service *node.Service,
	record dumpRecord,
	data map[string]any,
	hfid []string,
	branch string,
) (*node.Node, error) {
	if record.ID == "" {
		return service.Upsert(ctx, record.Kind, data, branch)
	}
	_, err := service.GetByID(ctx, record.Kind, record.ID, branch)
	if err == nil {
		data["id"] = record.ID
		return service.Upsert(ctx, record.Kind, data, branch)
	}
	var notFound *infrahub.NotFoundError
	if !errors.As(err, &notFound) {
		return nil, err
	}
	if len(hfid) > 0 {
		existing, lookupErr := service.GetByHFID(ctx, record.Kind, hfid, branch)
		if lookupErr == nil {
			data["id"] = existing.ID
			return service.Upsert(ctx, record.Kind, data, branch)
		}
		if !errors.As(lookupErr, &notFound) {
			return nil, lookupErr
		}
	}
	return service.Create(ctx, record.Kind, data, branch)
}

// relationshipNamesByKind indexes relationship names by schema kind.
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

// menuDocument holds internal data used by the menu document workflow.
type menuDocument struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Spec       struct {
		Kind string           `json:"kind" yaml:"kind"`
		Data []map[string]any `json:"data" yaml:"data"`
	} `json:"spec" yaml:"spec"`
}

// readMenu reads the menu.
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

// prepareMenuItem prepares the menu item.
func prepareMenuItem(item map[string]any, index int) error {
	if err := validateMenuItemRequiredFields(item); err != nil {
		return err
	}
	setMenuItemDefaults(item, index)
	return prepareMenuItemChildren(item)
}

// validateMenuItemRequiredFields validates the menu item required fields.
func validateMenuItemRequiredFields(item map[string]any) error {
	for _, key := range []string{"namespace", "name", "label"} {
		if value, ok := item[key].(string); !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a non-empty string", key)
		}
	}
	return nil
}

// setMenuItemDefaults sets the menu item defaults.
func setMenuItemDefaults(item map[string]any, index int) {
	if _, ok := item["order_weight"]; !ok {
		item["order_weight"] = (index + 1) * 1000
	}
	if _, ok := item["path"]; !ok {
		if kind, ok := item["kind"].(string); ok && kind != "" {
			item["path"] = "/objects/" + kind
		}
	}
}

// prepareMenuItemChildren prepares the menu item children.
func prepareMenuItemChildren(item map[string]any) error {
	children, ok := item["children"].([]any)
	if !ok {
		return nil
	}
	for childIndex, raw := range children {
		child, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("children[%d] must be an object", childIndex)
		}
		if err := prepareMenuItem(child, childIndex); err != nil {
			return err
		}
	}
	return nil
}

// runMenu runs the menu.
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

// runTelemetry runs the telemetry.
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

// runValidate runs the valIDate.
func (r Runner) runValidate(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) < 2 {
		return r.usageError("usage: infrahubctl validate <schema|graphql-query> <file> [flags]")
	}
	var exitCode int
	switch args[0] {
	case "schema":
		exitCode = r.runValidateSchema(args[1:])
	case "graphql-query":
		exitCode = r.runValidateGraphQL(ctx, client, branch, args[1:])
	default:
		exitCode = r.usageError("infrahubctl: unknown validate command " + args[0])
	}
	return exitCode
}

// runValidateSchema runs the valIDate schema.
func (r Runner) runValidateSchema(paths []string) int {
	documents, err := readSchemaDocuments(paths)
	if err != nil {
		return r.fail(err)
	}
	if err := validateSchemaDocuments(documents); err != nil {
		return r.fail(err)
	}
	return r.writeJSON(map[string]any{"valid": true, "documents": len(documents)})
}

// validateSchemaDocuments validates the schema documents.
func validateSchemaDocuments(documents []map[string]any) error {
	for index, document := range documents {
		if len(document) == 0 {
			return fmt.Errorf("schema document %d is empty", index)
		}
		_, nodes := document["nodes"]
		_, generics := document["generics"]
		if !nodes && !generics {
			return fmt.Errorf("schema document %d must define nodes or generics", index)
		}
	}
	return nil
}

// graphqlValidationOptions holds internal data used by the graphql valIDation options workflow.
type graphqlValidationOptions struct {
	branch    string
	out       string
	queryPath string
	variables []string
}

// runValidateGraphQL runs the valIDate GraphQL.
func (r Runner) runValidateGraphQL(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	flags := flag.NewFlagSet("validate graphql-query", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	commandBranch, out := flags.String("branch", branch, "target branch"), flags.String("out", "", "output file")
	var variables multiFlag
	flags.Var(&variables, "variable", "GraphQL variable key=value")
	if err := parseInterspersed(flags, args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 1 {
		return r.usageError("usage: infrahubctl validate graphql-query [flags] <query-file>")
	}
	return r.executeGraphQLValidation(ctx, client, graphqlValidationOptions{
		branch: *commandBranch, out: *out, queryPath: flags.Arg(0), variables: variables,
	})
}

// executeGraphQLValidation executes the GraphQL valIDation.
func (r Runner) executeGraphQLValidation(
	ctx context.Context,
	client *infrahub.Client,
	options graphqlValidationOptions,
) int {
	query, err := os.ReadFile(options.queryPath)
	if err != nil {
		return r.fail(err)
	}
	values, err := parseAssignments(options.variables, false)
	if err != nil {
		return r.usageError(err.Error())
	}
	var result any
	if err := client.Execute(ctx, infrahub.GraphQLRequest{
		Query: string(query), Variables: values, Branch: options.branch,
	}, &result); err != nil {
		return r.fail(err)
	}
	return r.writeGraphQLValidationResult(result, options.out)
}

// writeGraphQLValidationResult writes the GraphQL valIDation result.
func (r Runner) writeGraphQLValidationResult(result any, out string) int {
	if out == "" {
		return r.writeJSON(result)
	}
	if err := writeIndentedJSON(out, result); err != nil {
		return r.fail(err)
	}
	return 0
}

// writeIndentedJSON writes the indented JSON.
func writeIndentedJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// runProtocols runs the protocols.
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

// mergeSchemaDocuments merges the schema documents.
func mergeSchemaDocuments(documents []map[string]any) map[string]any {
	merged := map[string]any{"nodes": []any{}, "generics": []any{}}
	for _, document := range documents {
		for _, section := range []string{"nodes", "generics"} {
			if items, ok := document[section].([]any); ok {
				current, _ := merged[section].([]any)
				merged[section] = append(current, items...)
			}
		}
	}
	return merged
}

// protocolField holds internal data used by the protocol field workflow.
type protocolField struct {
	Name string
	Type string
}

// protocolDefinition holds internal data used by the protocol definition workflow.
type protocolDefinition struct {
	Kind   string
	Fields []protocolField
}

// generateProtocols generates the protocols.
func generateProtocols(schema map[string]any, syncMode bool) (string, error) {
	definitions := protocolDefinitions(schema)
	if len(definitions) == 0 {
		return "", fmt.Errorf("schema does not contain node or generic definitions")
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Kind < definitions[j].Kind })
	return renderProtocols(definitions, syncMode), nil
}

// protocolDefinitions converts schema kinds into protocol definitions.
func protocolDefinitions(schema map[string]any) []protocolDefinition {
	var definitions []protocolDefinition
	for _, section := range []string{"generics", "nodes"} {
		items, _ := schema[section].([]any)
		for _, raw := range items {
			if definition, ok := newProtocolDefinition(raw); ok {
				definitions = append(definitions, definition)
			}
		}
	}
	return definitions
}

// newProtocolDefinition creates the protocol definition.
func newProtocolDefinition(raw any) (protocolDefinition, bool) {
	item, ok := raw.(map[string]any)
	if !ok {
		return protocolDefinition{}, false
	}
	kind := protocolKind(item)
	if kind == "" {
		return protocolDefinition{}, false
	}
	fields := append(protocolAttributeFields(item), protocolRelationshipFields(item)...)
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return protocolDefinition{Kind: kind, Fields: fields}, true
}

// protocolKind converts one schema kind into a protocol definition.
func protocolKind(item map[string]any) string {
	kind, _ := item["kind"].(string)
	if kind != "" {
		return kind
	}
	namespace, _ := item["namespace"].(string)
	name, _ := item["name"].(string)
	return namespace + name
}

// protocolAttributeFields converts schema attributes into protocol fields.
func protocolAttributeFields(item map[string]any) []protocolField {
	attributes, _ := item["attributes"].([]any)
	fields := make([]protocolField, 0, len(attributes))
	for _, raw := range attributes {
		attribute, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := attribute["name"].(string); ok {
			fields = append(fields, protocolField{Name: name, Type: protocolAttributeType(attribute)})
		}
	}
	return fields
}

// protocolRelationshipFields converts schema relationships into protocol fields.
func protocolRelationshipFields(item map[string]any) []protocolField {
	relationships, _ := item["relationships"].([]any)
	fields := make([]protocolField, 0, len(relationships))
	for _, raw := range relationships {
		if field, ok := protocolRelationshipField(raw); ok {
			fields = append(fields, field)
		}
	}
	return fields
}

// protocolRelationshipField converts one schema relationship into a protocol field.
func protocolRelationshipField(raw any) (protocolField, bool) {
	relationship, _ := raw.(map[string]any)
	name, _ := relationship["name"].(string)
	peer, _ := relationship["peer"].(string)
	if name == "" || peer == "" {
		return protocolField{}, false
	}
	fieldType := peer
	if cardinality, _ := relationship["cardinality"].(string); cardinality == "many" {
		fieldType = "Sequence[" + peer + "]"
	}
	if optional, _ := relationship["optional"].(bool); optional {
		fieldType += " | None"
	}
	return protocolField{Name: name, Type: fieldType}, true
}

// renderProtocols renders the protocols.
func renderProtocols(definitions []protocolDefinition, syncMode bool) string {
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
	return output.String()
}

// protocolAttributeType maps an Infrahub attribute kind to a protocol field type.
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

// marketplaceOptions holds internal data used by the marketplace options workflow.
type marketplaceOptions struct {
	command    string
	baseURL    string
	collection bool
	query      string
	out        string
	limit      int
	args       []string
}

// runMarketplace runs the marketplace.
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
	options := marketplaceOptions{
		command:    args[0],
		baseURL:    *base,
		collection: *collection,
		query:      *query,
		out:        *out,
		limit:      *limit,
		args:       flags.Args(),
	}
	requestURL, err := marketplaceURL(options)
	if err != nil {
		return r.usageError(err.Error())
	}
	body, err := fetchMarketplace(ctx, requestURL)
	if err != nil {
		return r.fail(err)
	}
	if err := r.writeMarketplaceResponse(options, body); err != nil {
		return r.fail(err)
	}
	return 0
}

// marketplaceURL validates and constructs the marketplace request URL.
func marketplaceURL(options marketplaceOptions) (string, error) {
	parsed, err := url.Parse(options.baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("--url must be an absolute HTTP(S) URL")
	}
	path, values, err := marketplaceEndpoint(options)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

// marketplaceEndpoint appends an escaped endpoint path to the marketplace base URL.
func marketplaceEndpoint(options marketplaceOptions) (string, url.Values, error) {
	path := "/api/v1/schemas"
	if options.collection {
		path = "/api/v1/collections"
	}
	values := url.Values{}
	switch options.command {
	case "list":
		if len(options.args) != 0 {
			return "", nil, fmt.Errorf("usage: infrahubctl marketplace list [flags]")
		}
		values.Set("limit", strconv.Itoa(options.limit))
	case "search":
		query := marketplaceSearchQuery(options)
		if query == "" {
			return "", nil, fmt.Errorf("marketplace search requires a query")
		}
		values.Set("search", query)
		values.Set("limit", strconv.Itoa(options.limit))
	case "show", "get":
		identifier, err := marketplaceIdentifier(options)
		if err != nil {
			return "", nil, err
		}
		path += identifier
		if options.command == "get" && !options.collection {
			path += "/download"
		}
	default:
		return "", nil, fmt.Errorf("infrahubctl: unknown marketplace command %s", options.command)
	}
	return path, values, nil
}

// marketplaceSearchQuery builds marketplace search parameters from command options.
func marketplaceSearchQuery(options marketplaceOptions) string {
	if options.query == "" && len(options.args) == 1 {
		return options.args[0]
	}
	return options.query
}

// marketplaceIdentifier selects the requested marketplace object identifier.
func marketplaceIdentifier(options marketplaceOptions) (string, error) {
	if len(options.args) != 1 {
		return "", fmt.Errorf("marketplace %s requires namespace/name", options.command)
	}
	parts := strings.Split(options.args[0], "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("marketplace identifier must use namespace/name")
	}
	return "/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}

// fetchMarketplace fetches the marketplace.
func fetchMarketplace(ctx context.Context, requestURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "infrahubctl")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 16<<20 {
		return nil, fmt.Errorf("marketplace response exceeds 16 MiB")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("marketplace returned HTTP %d", response.StatusCode)
	}
	return body, nil
}

// writeMarketplaceResponse writes the marketplace response.
func (r Runner) writeMarketplaceResponse(options marketplaceOptions, body []byte) error {
	if options.command == "get" && options.out != "" {
		return os.WriteFile(options.out, body, 0o600)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		body = append(pretty.Bytes(), '\n')
	}
	_, err := r.Stdout.Write(body)
	return err
}
