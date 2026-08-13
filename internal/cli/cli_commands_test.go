package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cliRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	OperationName string         `json:"operationName"`
}

func runCLI(t *testing.T, server *httptest.Server, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	allArgs := append([]string{"-address", server.URL}, args...)
	exitCode := testRunner(&stdout, &stderr).Run(t.Context(), allArgs)
	return stdout.String(), stderr.String(), exitCode
}

func decodeCLIRequest(t *testing.T, r *http.Request) cliRequest {
	t.Helper()
	var request cliRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestObjectCreateSendsGeneratedMutation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/graphql/feature" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		request := decodeCLIRequest(t, r)
		if request.OperationName != "BuiltinTagCreate" || !strings.Contains(request.Query, "$data: BuiltinTagCreateInput!") {
			t.Fatalf("request = %#v", request)
		}
		data := request.Variables["data"].(map[string]any)
		name := data["name"].(map[string]any)
		if name["value"] != "staging" {
			t.Fatalf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTagCreate":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}}}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "object", "create", "BuiltinTag", "--branch", "feature", "--set", "name=staging")
	if exitCode != 0 || !strings.Contains(stdout, `"id": "tag-id"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestObjectLoadUpsertsObjectDocuments(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "objects.yml")
	if err := os.WriteFile(path, []byte(`
apiVersion: infrahub.app/v1
kind: Object
spec:
  kind: BuiltinTag
  data:
    - name: staging
`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := decodeCLIRequest(t, r)
		if request.OperationName != "BuiltinTagUpsert" || !strings.Contains(request.Query, "$data: BuiltinTagUpsertInput!") {
			t.Fatalf("request = %#v", request)
		}
		data := request.Variables["data"].(map[string]any)
		name := data["name"].(map[string]any)
		if name["value"] != "staging" {
			t.Fatalf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"BuiltinTagUpsert":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag","hfid":["staging"],"display_label":"staging"}}}}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "object", "load", path, "--branch", "feature")
	if exitCode != 0 || !strings.Contains(stdout, `"count": 1`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestSchemaLoadPostsSchemaDocuments(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "schema.yml")
	if err := os.WriteFile(path, []byte(`---
version: "1.0"
nodes: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.EscapedPath() != "/api/schema/load" || r.URL.Query().Get("branch") != "feature" {
			t.Fatalf("url = %s", r.URL.String())
		}
		var payload struct {
			Schemas []map[string]any `json:"schemas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Schemas) != 1 || payload.Schemas[0]["version"] != "1.0" {
			t.Fatalf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "schema", "load", path, "--branch", "feature")
	if exitCode != 0 || !strings.Contains(stdout, `"ok": true`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestTaskListSendsFilters(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := decodeCLIRequest(t, r)
		if request.OperationName != "TaskList" || !strings.Contains(request.Query, "logs { edges") {
			t.Fatalf("request = %#v", request)
		}
		states := request.Variables["state"].([]any)
		if len(states) != 1 || states[0] != "RUNNING" || request.Variables["limit"] != float64(5) {
			t.Fatalf("variables = %#v", request.Variables)
		}
		_, _ = w.Write([]byte(`{"data":{"InfrahubTask":{"count":0,"edges":[]}}}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "task", "list", "--state", "running", "--limit", "5", "--include-logs")
	if exitCode != 0 || !strings.Contains(stdout, `"Count": 0`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestObjectStoreUploadReadsStdin(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/api/storage/upload/content" {
			t.Fatalf("method=%s path=%s", r.Method, r.URL.EscapedPath())
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["content"] != "hello" {
			t.Fatalf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"identifier":"object-id","checksum":"sha256:abc"}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	runner.Stdin = strings.NewReader("hello")
	exitCode := runner.Run(t.Context(), []string{"-address", server.URL, "objectstore", "upload"})
	if exitCode != 0 || !strings.Contains(stdout.String(), `"identifier": "object-id"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestRepositoryListUsesSelectedBranch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/graphql/feature" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		request := decodeCLIRequest(t, r)
		if request.OperationName != "ListCoreGenericRepository" {
			t.Fatalf("request = %#v", request)
		}
		_, _ = w.Write([]byte(`{"data":{"CoreGenericRepository":{"count":1,"edges":[{"node":{"id":"repo-id","kind":"CoreRepository","name":{"value":"demo"},"location":{"value":"https://example.com/repo.git"},"commit":{"value":"abc"},"ref":{"value":""},"internal_status":{"value":"active"}}}]}}}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "repository", "list", "--branch", "feature")
	if exitCode != 0 || !strings.Contains(stdout, `"Name": "demo"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestDiffSummaryQueriesSelectedBranch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/graphql/feature" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		request := decodeCLIRequest(t, r)
		if request.OperationName != "GetDiffTree" || request.Variables["branch"] != "feature" {
			t.Fatalf("request = %#v", request)
		}
		_, _ = w.Write([]byte(`{"data":{"DiffTree":{"nodes":[{"uuid":"node-id","kind":"BuiltinTag","status":"ADDED","label":"staging","num_added":1,"num_updated":0,"num_removed":0,"attributes":[],"relationships":[]}]}}}`))
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "diff", "summary", "feature")
	if exitCode != 0 || !strings.Contains(stdout, `"Kind": "BuiltinTag"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}
