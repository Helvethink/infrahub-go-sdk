// Package diff provides branch diff summaries and complete diff trees.
package diff

import (
	"context"
	"fmt"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

// Action is the status of a changed node, attribute, relationship, or peer.
type Action string

const (
	// ActionAdded identifies an added object or field.
	ActionAdded Action = "ADDED"
	// ActionUpdated identifies an updated object or field.
	ActionUpdated Action = "UPDATED"
	// ActionRemoved identifies a removed object or field.
	ActionRemoved Action = "REMOVED"
	// ActionUnchanged identifies an unchanged object or field.
	ActionUnchanged Action = "UNCHANGED"
	// ActionConflict identifies a conflicting change.
	ActionConflict Action = "CONFLICT"
)

// ElementType identifies an attribute or relationship change.
type ElementType string

const (
	// ElementTypeAttribute identifies an attribute diff.
	ElementTypeAttribute ElementType = "ATTRIBUTE"
	// ElementTypeRelationshipOne identifies a cardinality-one relationship diff.
	ElementTypeRelationshipOne ElementType = "RELATIONSHIP_ONE"
	// ElementTypeRelationshipMany identifies a cardinality-many relationship diff.
	ElementTypeRelationshipMany ElementType = "RELATIONSHIP_MANY"
)

// Counts summarizes additions, updates, and removals.
type Counts struct {
	// Added, Updated, and Removed count changes by action.
	Added, Updated, Removed int
}

// Peer describes one changed peer of a cardinality-many relationship.
type Peer struct {
	// Action contains the action value.
	Action Action
	// Summary contains the summary value.
	Summary Counts
}

// Element describes one changed attribute or relationship.
type Element struct {
	// Name is the human-readable name.
	Name string
	// Type contains the type value.
	Type ElementType
	// Action contains the action value.
	Action Action
	// Summary contains the summary value.
	Summary Counts
	// Peers contains the peers value.
	Peers []Peer
}

// Node describes changes to one Infrahub node.
type Node struct {
	// Branch, Kind, ID contain the corresponding node values.
	Branch, Kind, ID string
	// Action contains the action value.
	Action Action
	// DisplayLabel is the human-readable display label.
	DisplayLabel string
	// Summary contains the summary value.
	Summary Counts
	// Elements contains the elements value.
	Elements []Element
}

// Tree contains complete diff metadata and changed nodes.
type Tree struct {
	// Name is the human-readable name.
	Name *string
	// FromTime, ToTime contain the corresponding tree values.
	FromTime, ToTime time.Time
	// BaseBranch, DiffBranch contain the corresponding tree values.
	BaseBranch, DiffBranch string
	// Added, Updated, Removed, Conflicts contain the corresponding tree values.
	Added, Updated, Removed, Conflicts int
	// UntrackedBaseChanges, UntrackedDiffChanges contain the corresponding tree values.
	UntrackedBaseChanges, UntrackedDiffChanges int
	// Nodes contains the nodes in this result.
	Nodes []Node
}

// Options selects a stored or time-bounded branch diff.
type Options struct {
	// Branch, Name contain the corresponding options values.
	Branch, Name string
	// FromTime, ToTime contain the corresponding options values.
	FromTime, ToTime time.Time
}

// Executor is the minimal GraphQL behavior required by Service.
type Executor interface {
	// Execute runs a GraphQL operation and decodes its data.
	Execute(context.Context, api.GraphQLRequest, any) error
}

// Service retrieves Infrahub branch diffs.
type Service struct{ client Executor }

// NewService creates a diff service backed by client.
func NewService(client Executor) *Service { return &Service{client: client} }

// Summary returns changed nodes without the tree's global metadata. An absent diff is an empty slice.
func (s *Service) Summary(ctx context.Context, options Options) ([]Node, error) {
	tree, err := s.execute(ctx, options, false)
	if tree == nil {
		return []Node{}, err
	}
	return tree.Nodes, err
}

// Tree returns the complete diff tree. It returns nil when no diff exists.
// If GraphQL returns partial data and errors, both the tree and error return.
func (s *Service) Tree(ctx context.Context, options Options) (*Tree, error) {
	return s.execute(ctx, options, true)
}

const nodeFields = `uuid kind status label num_added num_updated num_removed attributes { name status num_added num_updated num_removed } relationships { name status cardinality num_added num_updated num_removed elements { status num_added num_updated num_removed } }`

// execute retrieves and converts a diff tree with optional global metadata.
func (s *Service) execute(ctx context.Context, options Options, metadata bool) (*Tree, error) {
	if options.Branch == "" {
		return nil, fmt.Errorf("infrahub: diff branch must not be empty")
	}
	if !options.FromTime.IsZero() && !options.ToTime.IsZero() && options.FromTime.After(options.ToTime) {
		return nil, fmt.Errorf("infrahub: diff from time must not be after to time")
	}
	selection := "nodes { " + nodeFields + " }"
	if metadata {
		selection = "name from_time to_time base_branch diff_branch num_added num_updated num_removed num_conflicts num_untracked_base_changes num_untracked_diff_changes " + selection
	}
	var response struct {
		Tree *rawTree `json:"DiffTree"`
	}
	err := s.client.Execute(ctx, api.GraphQLRequest{
		Query:         `query GetDiffTree($branch: String!, $name: String, $fromTime: DateTime, $toTime: DateTime) { DiffTree(branch: $branch, name: $name, from_time: $fromTime, to_time: $toTime) { ` + selection + ` } }`,
		Variables:     map[string]any{"branch": options.Branch, "name": nullableString(options.Name), "fromTime": nullableTime(options.FromTime), "toTime": nullableTime(options.ToTime)},
		OperationName: "GetDiffTree", Branch: options.Branch, Tracker: "query-diff-tree",
	}, &response)
	if response.Tree == nil {
		return nil, err
	}
	return response.Tree.convert(options.Branch), err
}

// rawCounts holds internal data used by the raw counts workflow.
type rawCounts struct {
	Added   int `json:"num_added"`
	Updated int `json:"num_updated"`
	Removed int `json:"num_removed"`
}

// convert maps wire count fields to the public Counts type.
func (c rawCounts) convert() Counts {
	return Counts(c)
}

// rawPeer holds internal data used by the raw peer workflow.
type rawPeer struct {
	Status Action `json:"status"`
	rawCounts
}

// rawAttribute holds internal data used by the raw attribute workflow.
type rawAttribute struct {
	Name   string `json:"name"`
	Status Action `json:"status"`
	rawCounts
}

// rawRelationship holds internal data used by the raw relationship workflow.
type rawRelationship struct {
	Name        string    `json:"name"`
	Status      Action    `json:"status"`
	Cardinality string    `json:"cardinality"`
	Elements    []rawPeer `json:"elements"`
	rawCounts
}

// rawNode holds internal data used by the raw node workflow.
type rawNode struct {
	UUID          string            `json:"uuid"`
	Kind          string            `json:"kind"`
	Status        Action            `json:"status"`
	Label         string            `json:"label"`
	Attributes    []rawAttribute    `json:"attributes"`
	Relationships []rawRelationship `json:"relationships"`
	rawCounts
}

// rawTree holds internal data used by the raw tree workflow.
type rawTree struct {
	Name                 *string   `json:"name"`
	FromTime             time.Time `json:"from_time"`
	ToTime               time.Time `json:"to_time"`
	BaseBranch           string    `json:"base_branch"`
	DiffBranch           string    `json:"diff_branch"`
	Added                int       `json:"num_added"`
	Updated              int       `json:"num_updated"`
	Removed              int       `json:"num_removed"`
	Conflicts            int       `json:"num_conflicts"`
	UntrackedBaseChanges int       `json:"num_untracked_base_changes"`
	UntrackedDiffChanges int       `json:"num_untracked_diff_changes"`
	Nodes                []rawNode `json:"nodes"`
}

// convert maps a wire diff tree to its stable public representation.
func (t rawTree) convert(branch string) *Tree {
	tree := &Tree{Name: t.Name, FromTime: t.FromTime, ToTime: t.ToTime, BaseBranch: t.BaseBranch, DiffBranch: t.DiffBranch, Added: t.Added, Updated: t.Updated, Removed: t.Removed, Conflicts: t.Conflicts, UntrackedBaseChanges: t.UntrackedBaseChanges, UntrackedDiffChanges: t.UntrackedDiffChanges, Nodes: make([]Node, 0, len(t.Nodes))}
	for _, raw := range t.Nodes {
		node := Node{Branch: branch, Kind: raw.Kind, ID: raw.UUID, Action: raw.Status, DisplayLabel: raw.Label, Summary: raw.convert(), Elements: make([]Element, 0, len(raw.Attributes)+len(raw.Relationships))}
		for _, attribute := range raw.Attributes {
			node.Elements = append(node.Elements, Element{Name: attribute.Name, Type: ElementTypeAttribute, Action: attribute.Status, Summary: attribute.convert()})
		}
		for _, relationship := range raw.Relationships {
			elementType := ElementTypeRelationshipMany
			if relationship.Cardinality == "ONE" {
				elementType = ElementTypeRelationshipOne
			}
			element := Element{Name: relationship.Name, Type: elementType, Action: relationship.Status, Summary: relationship.convert()}
			if elementType == ElementTypeRelationshipMany {
				element.Peers = make([]Peer, 0, len(relationship.Elements))
				for _, peer := range relationship.Elements {
					element.Peers = append(element.Peers, Peer{Action: peer.Status, Summary: peer.convert()})
				}
			}
			node.Elements = append(node.Elements, element)
		}
		tree.Nodes = append(tree.Nodes, node)
	}
	return tree
}

// nullableString converts an empty string to a GraphQL null value.
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableTime converts a zero time to a GraphQL null value.
func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
