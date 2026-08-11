package node

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/internal/requestcontext"
	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

// Selection describes one field in a dynamic GraphQL selection set. Fields
// contains nested selections for attributes, relationships, or other objects.
type Selection struct {
	Name   string
	Fields []Selection
}

// Select creates a validated-at-query-time field selection.
//
// For example, Select("name", Select("value")) renders `name { value }`.
func Select(name string, fields ...Selection) Selection {
	return Selection{Name: name, Fields: fields}
}

// Filter describes one dynamic Infrahub query argument. Type may be empty for
// supported Go scalar and slice values. Set Type for custom scalars such as
// BigInt, DateTime, or nested list types.
type Filter struct {
	Name  string
	Value any
	Type  string
}

// QueryOptions configures a dynamic node query.
type QueryOptions struct {
	Branch     string
	Offset     int
	Limit      int
	Filters    []Filter
	Selections []Selection
}

// Query filters nodes of kind and returns caller-selected dynamic fields.
// Identity fields are always included. Filter values are sent as GraphQL
// variables and are never interpolated into the query document.
func (s *Service) Query(ctx context.Context, kind string, options QueryOptions) (*Page, error) {
	request, err := buildQuery(kind, options)
	if err != nil {
		return nil, err
	}
	page, err := s.queryPage(ctx, kind, request)
	if page != nil {
		page.Offset, page.Limit = options.Offset, options.Limit
		ids := make([]string, 0, len(page.Nodes))
		for _, item := range page.Nodes {
			ids = append(ids, item.ID)
		}
		requestcontext.RecordNodeIDs(ctx, ids...)
	}
	return page, err
}

func buildQuery(kind string, options QueryOptions) (api.GraphQLRequest, error) {
	if err := validateKind(kind); err != nil {
		return api.GraphQLRequest{}, err
	}
	if options.Offset < 0 || options.Limit < 0 {
		return api.GraphQLRequest{}, fmt.Errorf("infrahub: offset and limit must not be negative")
	}
	selection, err := renderSelections(options.Selections)
	if err != nil {
		return api.GraphQLRequest{}, err
	}

	definitions := []string{"$offset: Int", "$limit: Int"}
	arguments := []string{"offset: $offset", "limit: $limit"}
	variables := map[string]any{"offset": options.Offset, "limit": options.Limit}
	seenFilters := make(map[string]struct{}, len(options.Filters))
	for index, filter := range options.Filters {
		if !isGraphQLName(filter.Name) {
			return api.GraphQLRequest{}, fmt.Errorf("infrahub: invalid filter name %q", filter.Name)
		}
		if _, exists := seenFilters[filter.Name]; exists {
			return api.GraphQLRequest{}, fmt.Errorf("infrahub: duplicate filter %q", filter.Name)
		}
		seenFilters[filter.Name] = struct{}{}
		variableType := filter.Type
		if variableType == "" {
			variableType, err = inferGraphQLType(filter.Value)
			if err != nil {
				return api.GraphQLRequest{}, fmt.Errorf("infrahub: filter %q: %w", filter.Name, err)
			}
		} else if !isGraphQLType(variableType) {
			return api.GraphQLRequest{}, fmt.Errorf("infrahub: invalid GraphQL type %q for filter %q", variableType, filter.Name)
		}
		variableName := "filter" + strconv.Itoa(index)
		definitions = append(definitions, "$"+variableName+": "+variableType)
		arguments = append(arguments, filter.Name+": $"+variableName)
		variables[variableName] = filter.Value
	}

	operation := "Query" + kind
	document := "query " + operation + "(" + strings.Join(definitions, ", ") + ") { " +
		kind + "(" + strings.Join(arguments, ", ") + ") { count edges { node { " + selection + " } } } }"
	return api.GraphQLRequest{
		Query: document, Variables: variables, OperationName: operation, Branch: options.Branch,
	}, nil
}

func renderSelections(selections []Selection) (string, error) {
	result := []string{"id", "kind", "hfid", "display_label"}
	identity := map[string]struct{}{"id": {}, "kind": {}, "hfid": {}, "display_label": {}}
	seen := map[string]struct{}{"id": {}, "kind": {}, "hfid": {}, "display_label": {}}
	for _, selection := range selections {
		rendered, err := renderSelection(selection)
		if err != nil {
			return "", err
		}
		if _, exists := seen[selection.Name]; exists {
			if _, isIdentity := identity[selection.Name]; isIdentity {
				continue
			}
			return "", fmt.Errorf("infrahub: duplicate selection %q", selection.Name)
		}
		seen[selection.Name] = struct{}{}
		result = append(result, rendered)
	}
	return strings.Join(result, " "), nil
}

func renderSelection(selection Selection) (string, error) {
	if !isGraphQLName(selection.Name) {
		return "", fmt.Errorf("infrahub: invalid selection name %q", selection.Name)
	}
	if len(selection.Fields) == 0 {
		return selection.Name, nil
	}
	nested := make([]string, 0, len(selection.Fields))
	seen := make(map[string]struct{}, len(selection.Fields))
	for _, field := range selection.Fields {
		if _, exists := seen[field.Name]; exists {
			return "", fmt.Errorf("infrahub: duplicate nested selection %q in %q", field.Name, selection.Name)
		}
		seen[field.Name] = struct{}{}
		rendered, err := renderSelection(field)
		if err != nil {
			return "", err
		}
		nested = append(nested, rendered)
	}
	return selection.Name + " { " + strings.Join(nested, " ") + " }", nil
}

func inferGraphQLType(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return "String!", nil
	case bool:
		return "Boolean!", nil
	case int:
		return integerType(int64(typed))
	case int8:
		return "Int!", nil
	case int16:
		return "Int!", nil
	case int32:
		return "Int!", nil
	case int64:
		return integerType(typed)
	case float32, float64:
		return "Float!", nil
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integerType(integer)
		}
		if _, err := typed.Float64(); err == nil {
			return "Float!", nil
		}
		return "", fmt.Errorf("invalid JSON number %q", typed)
	case time.Time:
		return "DateTime!", nil
	case []string:
		return "[String!]!", nil
	case []bool:
		return "[Boolean!]!", nil
	case []int:
		for _, item := range typed {
			if _, err := integerType(int64(item)); err != nil {
				return "", err
			}
		}
		return "[Int!]!", nil
	case []float64:
		return "[Float!]!", nil
	default:
		return "", fmt.Errorf("cannot infer GraphQL type for %T; set Filter.Type explicitly", value)
	}
}

func integerType(value int64) (string, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return "", fmt.Errorf("integer %d exceeds GraphQL Int range; set Filter.Type to an Infrahub scalar such as BigInt", value)
	}
	return "Int!", nil
}

func isGraphQLName(value string) bool {
	if value == "" || value[0] != '_' && !isASCIIAlpha(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] != '_' && !isASCIIAlpha(value[index]) && (value[index] < '0' || value[index] > '9') {
			return false
		}
	}
	return true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isGraphQLType(value string) bool {
	position, ok := parseGraphQLType(value, 0)
	return ok && position == len(value)
}

func parseGraphQLType(value string, position int) (int, bool) {
	if position >= len(value) {
		return position, false
	}
	if value[position] == '[' {
		position, ok := parseGraphQLType(value, position+1)
		if !ok || position >= len(value) || value[position] != ']' {
			return position, false
		}
		position++
		if position < len(value) && value[position] == '!' {
			position++
		}
		return position, true
	}
	start := position
	for position < len(value) && (value[position] == '_' || isASCIIAlpha(value[position]) || position > start && value[position] >= '0' && value[position] <= '9') {
		position++
	}
	if position == start || !isGraphQLName(value[start:position]) {
		return position, false
	}
	if position < len(value) && value[position] == '!' {
		position++
	}
	return position, true
}
