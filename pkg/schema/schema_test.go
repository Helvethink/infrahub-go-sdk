package schema

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/api"
)

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
