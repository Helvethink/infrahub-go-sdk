// Package tracking provides request-scoped trackers and Infrahub group collection.
package tracking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

const defaultGroupKind = "CoreStandardGroup"

var kindPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// Executor is the minimal GraphQL behavior required to save a Group.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// WithTracker returns a child context that overrides the tracker header for
// every SDK operation executed with it. An empty tracker suppresses the header.
func WithTracker(ctx context.Context, tracker string) context.Context {
	return requestcontext.WithTracker(ctx, tracker)
}

// GroupOptions configures a request-scoped tracking group.
type GroupOptions struct {
	// Identifier is the object or resource identifier.
	Identifier string
	// Params contains the params value.
	Params map[string]string
	// Description contains the optional human-readable description.
	Description string
	// GroupKind contains the group kind value.
	GroupKind string
	// GroupFields contains the group fields value.
	GroupFields map[string]any
	// Branch selects or identifies the Infrahub branch.
	Branch string
}

// Group safely collects node IDs and persists them as an Infrahub group.
// Configuration is copied at construction and may be read concurrently.
type Group struct {
	identifier  string
	params      map[string]string
	description string
	kind        string
	fields      map[string]any
	branch      string

	mu      sync.Mutex
	members map[string]struct{}
}

// GroupResult identifies the group created or updated by Save.
type GroupResult struct {
	// ID is the stable Infrahub identifier.
	ID string `json:"id"`
	// Kind is the Infrahub schema kind.
	Kind string `json:"kind"`
	// DisplayLabel is the human-readable display label.
	DisplayLabel string `json:"display_label"`
}

// NewGroup creates an empty request-scoped group collector.
func NewGroup(options GroupOptions) (*Group, error) {
	if strings.TrimSpace(options.Identifier) == "" {
		return nil, fmt.Errorf("infrahub: tracking group identifier must not be empty")
	}
	kind := options.GroupKind
	if kind == "" {
		kind = defaultGroupKind
	}
	if !kindPattern.MatchString(kind) {
		return nil, fmt.Errorf("infrahub: invalid tracking group kind %q", kind)
	}
	params := make(map[string]string, len(options.Params))
	for key, value := range options.Params {
		params[key] = value
	}
	fields := make(map[string]any, len(options.GroupFields))
	for key, value := range options.GroupFields {
		if !isGraphQLName(key) || key == "name" || key == "description" || key == "members" {
			return nil, fmt.Errorf("infrahub: invalid or reserved tracking group field %q", key)
		}
		fields[key] = value
	}
	return &Group{
		identifier: options.Identifier, params: params, description: options.Description,
		kind: kind, fields: fields, branch: options.Branch, members: make(map[string]struct{}),
	}, nil
}

// Context returns a child context that records nodes returned by SDK services.
func (g *Group) Context(parent context.Context) context.Context {
	return requestcontext.WithRecorder(parent, g)
}

// RecordNodeIDs adds node IDs to the group. Empty IDs are ignored.
func (g *Group) RecordNodeIDs(ids ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range ids {
		if id != "" {
			g.members[id] = struct{}{}
		}
	}
}

// Members returns a sorted snapshot of collected node IDs.
func (g *Group) Members() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	members := make([]string, 0, len(g.members))
	for id := range g.members {
		members = append(members, id)
	}
	sort.Strings(members)
	return members
}

// Name returns the deterministic group name derived from identifier and params.
func (g *Group) Name() string {
	if len(g.params) == 0 {
		return g.identifier
	}
	encoded, _ := json.Marshal(g.params)
	digest := sha256.Sum256(encoded)
	return g.identifier + "-" + hex.EncodeToString(digest[:16])
}

// Save upserts the collected members and returns the resulting group. Save is
// a no-op when no members have been collected.
func (g *Group) Save(ctx context.Context, client Executor) (*GroupResult, error) {
	members := g.Members()
	if len(members) == 0 {
		return nil, nil
	}
	operation := g.kind + "Upsert"
	data := make(map[string]any, len(g.fields)+3)
	for key, value := range g.fields {
		data[key] = value
	}
	data["name"] = map[string]any{"value": g.Name()}
	data["description"] = map[string]any{"value": g.description}
	related := make([]map[string]string, 0, len(members))
	for _, id := range members {
		related = append(related, map[string]string{"id": id})
	}
	data["members"] = related

	var response map[string]struct {
		OK     bool         `json:"ok"`
		Object *GroupResult `json:"object"`
	}
	err := client.Execute(ctx, api.GraphQLRequest{
		Query:     `mutation ` + operation + `($data: ` + operation + `Input!) { ` + operation + `(data: $data) { ok object { id kind: __typename display_label } } }`,
		Variables: map[string]any{"data": data}, OperationName: operation, Branch: g.branch,
		Tracker: "mutation-tracking-group-upsert",
	}, &response)
	result := response[operation]
	if err != nil {
		return result.Object, err
	}
	if !result.OK || result.Object == nil {
		return nil, &api.OperationError{Operation: operation}
	}
	return result.Object, nil
}

// isGraphQLName reports whether value is a valid GraphQL name.
func isGraphQLName(value string) bool {
	if value == "" || value[0] != '_' && !isLetter(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] != '_' && !isLetter(value[index]) && (value[index] < '0' || value[index] > '9') {
			return false
		}
	}
	return true
}

// isLetter reports whether value is an ASCII letter.
func isLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
