// Package automation provides Go-native transforms, generators, and checks.
package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

// Client is the minimal REST behavior required by Service.
type Client interface {
	// EndpointSegments resolves individually escaped REST path segments.
	EndpointSegments([]string, url.Values) *url.URL
	// DoResponse executes a bounded HTTP request and preserves response metadata.
	DoResponse(context.Context, string, *url.URL, io.Reader, http.Header, string) (*api.HTTPResponse, error)
}

// Service collects named GraphQL-query data and runs Go-native automation.
type Service struct{ client Client }

// NewService creates an automation service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// QueryOptions configures execution of a stored CoreGraphQLQuery.
type QueryOptions struct {
	// Name is the human-readable name.
	Name string
	// Variables contains GraphQL variable values.
	Variables map[string]any
	// Parameters contains the parameters value.
	Parameters url.Values
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// At selects an optional point in time.
	At time.Time
	// UpdateGroup contains the update group value.
	UpdateGroup bool
	// Subscribers contains the subscribers value.
	Subscribers []string
}

// Query executes a named Infrahub GraphQL query and decodes its JSON response.
func (s *Service) Query(ctx context.Context, options QueryOptions, dst any) error {
	if options.Name == "" {
		return fmt.Errorf("infrahub: automation query name must not be empty")
	}
	if dst == nil {
		return fmt.Errorf("infrahub: automation query destination must not be nil")
	}
	query := cloneValues(options.Parameters)
	if options.Branch != "" {
		query.Set("branch", options.Branch)
	}
	if !options.At.IsZero() {
		query.Set("at", options.At.Format(time.RFC3339Nano))
	}
	query.Set("update_group", fmt.Sprintf("%t", options.UpdateGroup))
	for _, subscriber := range options.Subscribers {
		query.Add("subscribers", subscriber)
	}
	payload := make(map[string]any)
	if options.Variables != nil {
		payload["variables"] = options.Variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("infrahub: encode automation query variables: %w", err)
	}
	response, err := s.client.DoResponse(ctx, http.MethodPost,
		s.client.EndpointSegments([]string{"api", "query", options.Name}, query), bytes.NewReader(body),
		http.Header{"Content-Type": {"application/json"}}, "query-automation-"+options.Name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(response.Body, dst); err != nil {
		return fmt.Errorf("infrahub: decode automation query %q: %w", options.Name, err)
	}
	return nil
}

// Transform converts collected query data into an application result.
type Transform func(context.Context, map[string]any) (any, error)

// Generator creates or updates Infrahub resources from collected query data.
// Implementations should be idempotent because generators may run repeatedly.
type Generator func(context.Context, map[string]any) error

// Check validates collected query data and writes findings to Reporter.
type Check func(context.Context, map[string]any, *Reporter) error

// RunOptions configures collection for an automation execution.
type RunOptions struct {
	// Query configures the named query that supplies automation input.
	Query QueryOptions
}

// RunTransform collects query data and invokes transform.
func (s *Service) RunTransform(ctx context.Context, options RunOptions, transform Transform) (any, error) {
	if transform == nil {
		return nil, fmt.Errorf("infrahub: transform must not be nil")
	}
	data, err := s.collect(ctx, options.Query)
	if err != nil {
		return nil, err
	}
	return transform(ctx, data)
}

// RunGenerator collects query data and invokes generator. Callers may attach a
// tracking.Group to ctx so SDK mutations performed by generator are collected.
func (s *Service) RunGenerator(ctx context.Context, options RunOptions, generator Generator) error {
	if generator == nil {
		return fmt.Errorf("infrahub: generator must not be nil")
	}
	data, err := s.collect(ctx, options.Query)
	if err != nil {
		return err
	}
	return generator(ctx, data)
}

// Severity is the severity of a check finding.
type Severity string

const (
	// SeverityInfo identifies an informational finding.
	SeverityInfo Severity = "INFO"
	// SeverityWarning identifies a warning finding.
	SeverityWarning Severity = "WARNING"
	// SeverityError identifies an error finding.
	SeverityError Severity = "ERROR"
)

// Finding is one structured check message.
type Finding struct {
	// Severity contains the severity value.
	Severity Severity
	// Message contains the human-readable message.
	Message string
	// Branch selects or identifies the Infrahub branch.
	Branch string
	// ObjectID contains the object ID value.
	ObjectID string
	// ObjectType contains the object type value.
	ObjectType string
	// Sequence contains the sequence value.
	Sequence int
}

// Reporter safely collects findings from concurrent check work.
type Reporter struct {
	branch   string
	mu       sync.Mutex
	findings []Finding
}

// NewReporter creates an empty reporter for branch.
func NewReporter(branch string) *Reporter { return &Reporter{branch: branch} }

// Report adds a finding. An empty message is ignored.
func (r *Reporter) Report(severity Severity, message, objectID, objectType string) {
	if message == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, Finding{Severity: severity, Message: message, Branch: r.branch, ObjectID: objectID, ObjectType: objectType, Sequence: len(r.findings)})
}

// Info adds an informational finding.
func (r *Reporter) Info(message, objectID, objectType string) {
	r.Report(SeverityInfo, message, objectID, objectType)
}

// Warning adds a warning finding.
func (r *Reporter) Warning(message, objectID, objectType string) {
	r.Report(SeverityWarning, message, objectID, objectType)
}

// Error adds an error finding.
func (r *Reporter) Error(message, objectID, objectType string) {
	r.Report(SeverityError, message, objectID, objectType)
}

// Findings returns a deterministic snapshot ordered by report sequence.
func (r *Reporter) Findings() []Finding {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := append([]Finding(nil), r.findings...)
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result
}

// CheckResult contains a check's pass state and findings.
type CheckResult struct {
	// Passed reports whether the check completed without error findings.
	Passed bool
	// Findings contains findings emitted by the check.
	Findings []Finding
}

// RunCheck collects query data and invokes check. ERROR findings fail the
// result; warnings and info do not. A check execution error is returned directly.
func (s *Service) RunCheck(ctx context.Context, options RunOptions, check Check) (*CheckResult, error) {
	if check == nil {
		return nil, fmt.Errorf("infrahub: check must not be nil")
	}
	data, err := s.collect(ctx, options.Query)
	if err != nil {
		return nil, err
	}
	reporter := NewReporter(options.Query.Branch)
	if err := check(ctx, data, reporter); err != nil {
		return nil, err
	}
	findings := reporter.Findings()
	passed := true
	for _, finding := range findings {
		if finding.Severity == SeverityError {
			passed = false
			break
		}
	}
	if passed {
		reporter.Info("Check successfully completed", "", "")
		findings = reporter.Findings()
	}
	return &CheckResult{Passed: passed, Findings: findings}, nil
}

// collect executes a named query and extracts its data object when present.
func (s *Service) collect(ctx context.Context, options QueryOptions) (map[string]any, error) {
	var response map[string]any
	if err := s.Query(ctx, options, &response); err != nil {
		return nil, err
	}
	if data, ok := response["data"].(map[string]any); ok {
		return data, nil
	}
	return response, nil
}

// cloneValues clones the values.
func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}
