package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HTTPError reports a non-successful HTTP response.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("infrahub: %s %s returned HTTP %d", e.Method, e.URL, e.StatusCode)
}

// GraphQLErrorItem is one error returned by a GraphQL server.
type GraphQLErrorItem struct {
	Message    string                     `json:"message"`
	Path       []any                      `json:"path,omitempty"`
	Locations  []GraphQLErrorLocation     `json:"locations,omitempty"`
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// GraphQLErrorLocation identifies a location in a GraphQL document.
type GraphQLErrorLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// GraphQLError aggregates GraphQL errors. Data may still have been decoded.
type GraphQLError struct{ Items []GraphQLErrorItem }

func (e *GraphQLError) Error() string {
	messages := make([]string, 0, len(e.Items))
	for _, item := range e.Items {
		messages = append(messages, item.Message)
	}
	return "infrahub: GraphQL: " + strings.Join(messages, " | ")
}

// OperationError reports a mutation that completed without returning ok=true.
type OperationError struct{ Operation string }

func (e *OperationError) Error() string {
	return fmt.Sprintf("infrahub: operation %s did not succeed", e.Operation)
}

// NotFoundError reports that no object matched a stable identifier.
type NotFoundError struct {
	Kind       string
	Identifier string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("infrahub: %s %q not found", e.Kind, e.Identifier)
}
