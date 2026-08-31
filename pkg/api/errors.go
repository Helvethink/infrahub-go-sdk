package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HTTPError reports a non-successful HTTP response.
type HTTPError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Method is the HTTP method used by the failed request.
	Method string
	// URL is the redacted URL of the failed request.
	URL string
	// Body contains the bounded HTTP response body.
	Body string
}

// Error returns a safe description of the failed HTTP request.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("infrahub: %s %s returned HTTP %d", e.Method, e.URL, e.StatusCode)
}

// GraphQLErrorItem is one error returned by a GraphQL server.
type GraphQLErrorItem struct {
	// Message contains the human-readable message.
	Message string `json:"message"`
	// Path identifies the GraphQL response path.
	Path []any `json:"path,omitempty"`
	// Locations contains source locations associated with the GraphQL error.
	Locations []GraphQLErrorLocation `json:"locations,omitempty"`
	// Extensions contains server-defined GraphQL error metadata.
	Extensions map[string]json.RawMessage `json:"extensions,omitempty"`
}

// GraphQLErrorLocation identifies a location in a GraphQL document.
type GraphQLErrorLocation struct {
	// Line is the one-based GraphQL source line.
	Line int `json:"line"`
	// Column is the one-based GraphQL source column.
	Column int `json:"column"`
}

// GraphQLError aggregates GraphQL errors. Data may still have been decoded.
type GraphQLError struct {
	// Items contains the GraphQL errors returned by the server.
	Items []GraphQLErrorItem
}

// Error summarizes the GraphQL errors returned by Infrahub.
func (e *GraphQLError) Error() string {
	messages := make([]string, 0, len(e.Items))
	for _, item := range e.Items {
		messages = append(messages, item.Message)
	}
	return "infrahub: GraphQL: " + strings.Join(messages, " | ")
}

// OperationError reports a mutation that completed without returning ok=true.
type OperationError struct {
	// Operation names the unsuccessful Infrahub operation.
	Operation string
}

// Error reports that an Infrahub operation did not succeed.
func (e *OperationError) Error() string {
	return fmt.Sprintf("infrahub: operation %s did not succeed", e.Operation)
}

// NotFoundError reports that no object matched a stable identifier.
type NotFoundError struct {
	// Kind is the Infrahub schema kind.
	Kind string
	// Identifier is the object or resource identifier.
	Identifier string
}

// Error reports the kind and identifier of the missing object.
func (e *NotFoundError) Error() string {
	return fmt.Sprintf("infrahub: %s %q not found", e.Kind, e.Identifier)
}
