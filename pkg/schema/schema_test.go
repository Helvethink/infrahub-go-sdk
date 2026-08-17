package schema

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

type schemaCall struct {
	method   string
	endpoint *url.URL
	body     []byte
}

type schemaClient struct {
	branch   string
	response []byte
	err      error
	calls    []schemaCall
}

func (c *schemaClient) DefaultBranch() string { return c.branch }

func (c *schemaClient) Endpoint(path string, query url.Values) *url.URL {
	return &url.URL{Scheme: "https", Host: "example.com", Path: "/root/" + path, RawQuery: query.Encode()}
}

func (c *schemaClient) Do(_ context.Context, method string, endpoint *url.URL, body io.Reader, _ http.Header, _ string) ([]byte, error) {
	var data []byte
	if body != nil {
		data, _ = io.ReadAll(body)
	}
	c.calls = append(c.calls, schemaCall{method: method, endpoint: endpoint, body: data})
	return c.response, c.err
}

func TestGraphQLUsesEncodedBranchQuery(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/root/schema.graphql" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("branch") != "feature/a b" {
			t.Errorf("branch = %q", r.URL.Query().Get("branch"))
		}
		_, _ = w.Write([]byte("type Query { ok: Boolean! }"))
	}))
	defer server.Close()
	client, err := api.NewClient(server.URL+"/root", api.Config{
		HTTPClient: server.Client(), DefaultBranch: "main", UserAgent: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewService(client).GraphQL(context.Background(), "feature/a b")
	if err != nil || !strings.HasPrefix(result, "type Query") {
		t.Fatalf("GraphQL() = %q, %v", result, err)
	}
}

func TestFetchUsesDefaultBranchAndNamespaces(t *testing.T) {
	t.Parallel()
	client := &schemaClient{branch: "main", response: []byte(`{"nodes":[{"kind":"InfraDevice"}]}`)}
	var result map[string][]map[string]string
	if err := NewService(client).Fetch(context.Background(), "", []string{"Infra", "Core"}, &result); err != nil {
		t.Fatal(err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("calls = %d", len(client.calls))
	}
	call := client.calls[0]
	if call.method != http.MethodGet || call.endpoint.Path != "/root/api/schema" {
		t.Fatalf("call = %#v", call)
	}
	if got := call.endpoint.Query()["namespaces"]; !reflect.DeepEqual(got, []string{"Infra", "Core"}) {
		t.Fatalf("namespaces = %#v", got)
	}
	if got := call.endpoint.Query().Get("branch"); got != "main" {
		t.Fatalf("branch = %q", got)
	}
	if got := result["nodes"][0]["kind"]; got != "InfraDevice" {
		t.Fatalf("kind = %q", got)
	}
}

func TestFetchErrors(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("transport failed")
	client := &schemaClient{branch: "main", err: sentinel}
	if err := NewService(client).Fetch(context.Background(), "develop", nil, nil); !errors.Is(err, sentinel) {
		t.Fatalf("transport error = %v", err)
	}
	client.err = nil
	client.response = []byte("not-json")
	if err := NewService(client).Fetch(context.Background(), "develop", nil, &map[string]any{}); err == nil || !strings.Contains(err.Error(), "decode schema") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestLoadAndCheckRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		path   string
		branch string
		call   func(*Service, any) error
	}{
		{name: "load default branch", path: "/root/api/schema/load", call: func(service *Service, dst any) error {
			return service.Load(context.Background(), "", []map[string]any{{"name": "device"}}, dst)
		}},
		{name: "check explicit branch", path: "/root/api/schema/check", branch: "develop", call: func(service *Service, dst any) error {
			return service.Check(context.Background(), "develop", []map[string]any{{"name": "device"}}, dst)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &schemaClient{branch: "main", response: []byte(`{"ok":true}`)}
			var result struct {
				OK bool `json:"ok"`
			}
			if err := tt.call(NewService(client), &result); err != nil {
				t.Fatal(err)
			}
			call := client.calls[0]
			if call.method != http.MethodPost || call.endpoint.Path != tt.path || !bytes.Equal(call.body, []byte(`{"schemas":[{"name":"device"}]}`)) {
				t.Fatalf("call = %#v", call)
			}
			wantBranch := tt.branch
			if wantBranch == "" {
				wantBranch = "main"
			}
			if call.endpoint.Query().Get("branch") != wantBranch || !result.OK {
				t.Fatalf("branch=%q result=%#v", call.endpoint.Query().Get("branch"), result)
			}
		})
	}
}

func TestPostErrorsAndNilDestination(t *testing.T) {
	t.Parallel()
	service := NewService(&schemaClient{branch: "main", response: []byte("not-json")})
	if err := service.Load(context.Background(), "", nil, nil); err != nil {
		t.Fatalf("nil destination error = %v", err)
	}
	if err := service.Check(context.Background(), "", nil, &map[string]any{}); err == nil || !strings.Contains(err.Error(), "decode schema response") {
		t.Fatalf("decode error = %v", err)
	}
	if err := service.Load(context.Background(), "", []map[string]any{{"invalid": func() {}}}, nil); err == nil || !strings.Contains(err.Error(), "encode schemas") {
		t.Fatalf("encode error = %v", err)
	}
	sentinel := errors.New("transport failed")
	service = NewService(&schemaClient{branch: "main", err: sentinel})
	if err := service.Check(context.Background(), "", nil, nil); !errors.Is(err, sentinel) {
		t.Fatalf("transport error = %v", err)
	}
}

func TestGraphQLPropagatesTransportError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("transport failed")
	service := NewService(&schemaClient{branch: "main", err: sentinel})
	if _, err := service.GraphQL(context.Background(), ""); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
}
