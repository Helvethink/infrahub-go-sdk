// Package telemetry provides access to Infrahub telemetry snapshots.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const exportPageSize = 1000

// Client is the minimal REST behavior required by Service.
type Client interface {
	// Endpoint resolves an Infrahub REST endpoint against the configured base URL.
	Endpoint(string, url.Values) *url.URL
	// Do executes a bounded HTTP request.
	Do(context.Context, string, *url.URL, io.Reader, http.Header, string) ([]byte, error)
}

// Service retrieves telemetry snapshots from Infrahub.
type Service struct{ client Client }

// NewService creates a telemetry service backed by client.
func NewService(client Client) *Service { return &Service{client: client} }

// Snapshot is one telemetry payload. Fields not understood by this SDK are
// preserved because snapshot content evolves independently of the client.
type Snapshot map[string]any

// ListOptions filters and paginates telemetry snapshots.
type ListOptions struct {
	// StartDate sets the inclusive start of the telemetry range.
	StartDate time.Time
	// EndDate sets the inclusive end of the telemetry range.
	EndDate time.Time
	// Offset is the zero-based pagination offset.
	Offset int
	// Limit is the requested page size.
	Limit int
}

// Page contains one page of telemetry snapshots.
type Page struct {
	// Snapshots contains the telemetry snapshots in this page.
	Snapshots []Snapshot `json:"snapshots"`
	// Count is the total number of matching items.
	Count int `json:"count"`
}

// List retrieves one page of telemetry snapshots.
func (s *Service) List(ctx context.Context, options ListOptions) (*Page, error) {
	if options.Offset < 0 || options.Limit < 0 {
		return nil, fmt.Errorf("infrahub: telemetry offset and limit must not be negative")
	}
	if !options.StartDate.IsZero() && !options.EndDate.IsZero() && options.StartDate.After(options.EndDate) {
		return nil, fmt.Errorf("infrahub: telemetry start date must not be after end date")
	}
	query := url.Values{}
	if options.Limit > 0 {
		query.Set("limit", strconv.Itoa(options.Limit))
	}
	if options.Offset > 0 {
		query.Set("offset", strconv.Itoa(options.Offset))
	}
	if !options.StartDate.IsZero() {
		query.Set("start_date", options.StartDate.Format(time.RFC3339))
	}
	if !options.EndDate.IsZero() {
		query.Set("end_date", options.EndDate.Format(time.RFC3339))
	}
	body, err := s.client.Do(ctx, http.MethodGet, s.client.Endpoint("api/telemetry/snapshots", query), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var page Page
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("infrahub: decode telemetry snapshots: %w", err)
	}
	if page.Snapshots == nil {
		page.Snapshots = []Snapshot{}
	}
	return &page, nil
}

// All retrieves every telemetry snapshot matching the date filters.
func (s *Service) All(ctx context.Context, options ListOptions) ([]Snapshot, error) {
	options.Offset = 0
	options.Limit = exportPageSize
	result := make([]Snapshot, 0)
	for {
		page, err := s.List(ctx, options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Snapshots...)
		if len(page.Snapshots) < options.Limit || len(result) >= page.Count {
			return result, nil
		}
		options.Offset += options.Limit
	}
}
