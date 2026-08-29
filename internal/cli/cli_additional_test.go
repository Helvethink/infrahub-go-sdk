package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneralCommandsAndErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{name: "no command", wantExit: 2, wantStderr: "usage: infrahubctl"},
		{name: "global help flag", args: []string{"-h"}, wantExit: 0, wantStderr: "usage: infrahubctl"},
		{name: "unknown command", args: []string{"-address", "https://example.com", "unknown"}, wantExit: 2, wantStderr: `unknown command "unknown"`},
		{name: "info", args: []string{"-address", "https://example.com", "-branch", "feature", "info"}, wantExit: 0, wantStdout: `"default_branch": "feature"`},
		{name: "empty explicit config", args: []string{"-config=", "branch", "list"}, wantExit: 1, wantStderr: "config path must not be empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			exitCode := testRunner(&stdout, &stderr).Run(t.Context(), test.args)
			if exitCode != test.wantExit || !strings.Contains(stdout.String(), test.wantStdout) || !strings.Contains(stderr.String(), test.wantStderr) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestBranchCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		args          []string
		wantOperation string
		response      string
		wantStdout    string
	}{
		{name: "get", args: []string{"branch", "get", "feature"}, wantOperation: "BranchGet", response: `{"data":{"Branch":[{"id":"1","name":"feature","branched_from":"main","status":"OPEN"}]}}`, wantStdout: `"name": "feature"`},
		{name: "create", args: []string{"branch", "create", "feature", "--description", "work", "--sync-with-git"}, wantOperation: "BranchCreate", response: `{"data":{"BranchCreate":{"ok":true,"object":{"id":"1","name":"feature","branched_from":"main","sync_with_git":true,"status":"OPEN"}}}}`, wantStdout: `"sync_with_git": true`},
		{name: "delete", args: []string{"branch", "delete", "feature"}, wantOperation: "BranchDelete", response: `{"data":{"BranchDelete":{"ok":true}}}`, wantStdout: "delete: ok"},
		{name: "rebase", args: []string{"branch", "rebase", "feature"}, wantOperation: "BranchRebase", response: `{"data":{"BranchRebase":{"ok":true}}}`, wantStdout: "rebase: ok"},
		{name: "validate", args: []string{"branch", "validate", "feature"}, wantOperation: "BranchValidate", response: `{"data":{"BranchValidate":{"ok":true}}}`, wantStdout: "validate: ok"},
		{name: "merge", args: []string{"branch", "merge", "feature"}, wantOperation: "BranchMerge", response: `{"data":{"BranchMerge":{"ok":true}}}`, wantStdout: "merge: ok"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				payload := decodeCLIRequest(t, request)
				if payload.OperationName != test.wantOperation {
					t.Errorf("operation = %q", payload.OperationName)
				}
				if test.wantOperation == "BranchGet" && payload.Variables["name"] != "feature" {
					t.Errorf("variables = %#v", payload.Variables)
				}
				if test.wantOperation == "BranchCreate" {
					data, _ := payload.Variables["data"].(map[string]any)
					if data["description"] != "work" || data["sync_with_git"] != true {
						t.Errorf("variables = %#v", payload.Variables)
					}
				}
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()

			stdout, stderr, exitCode := runCLI(t, server, test.args...)
			if exitCode != 0 || !strings.Contains(stdout, test.wantStdout) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
		})
	}
}

func TestBranchReport(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.EscapedPath() != "/graphql/feature" {
			t.Errorf("request = %s %s", request.Method, request.URL.String())
		}
		var payload cliRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.OperationName != "BranchDiffData" || payload.Variables["branch"] != "feature" || payload.Variables["includeParents"] != true {
			t.Errorf("payload = %#v", payload)
		}
		_, _ = io.WriteString(w, `{"data":{"DiffTree":{"num_added":1,"nodes":[]}}}`)
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "branch", "report", "feature", "--update-diff")
	if exitCode != 0 || !strings.Contains(stdout, `"num_added": 1`) || !strings.Contains(stderr, "accepted for compatibility") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

func TestBranchCommandUsageErrors(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected request")
	}))
	defer server.Close()

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "missing command", args: []string{"branch"}, wantStderr: "branch <list|get|create"},
		{name: "unknown command", args: []string{"branch", "unknown"}, wantStderr: "branch <list|get|create"},
		{name: "get missing name", args: []string{"branch", "get"}, wantStderr: "branch get <name>"},
		{name: "get extra name", args: []string{"branch", "get", "one", "two"}, wantStderr: "branch get <name>"},
		{name: "create missing name", args: []string{"branch", "create"}, wantStderr: "branch create [flags] <name>"},
		{name: "create invalid flag", args: []string{"branch", "create", "--invalid"}},
		{name: "operation missing name", args: []string{"branch", "delete"}, wantStderr: "branch delete <name>"},
		{name: "report missing name", args: []string{"branch", "report"}, wantStderr: "branch report [flags] <name>"},
		{name: "report invalid flag", args: []string{"branch", "report", "--invalid"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, exitCode := runCLI(t, server, test.args...)
			if exitCode != 2 || !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
			}
		})
	}
}

func TestRunBranchRejectsUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).runBranch(t.Context(), nil, []string{"unknown"})
	if exitCode != 2 || !strings.Contains(stderr.String(), "unknown branch command unknown") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestSchemaCommands(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("branch") != "feature" {
			t.Errorf("branch = %q", request.URL.Query().Get("branch"))
		}
		switch request.URL.EscapedPath() {
		case "/schema.graphql":
			_, _ = io.WriteString(w, "type Query { ok: Boolean }")
		case "/api/schema":
			_, _ = io.WriteString(w, `{"nodes":[{"kind":"BuiltinTag"},{"kind":"CoreDevice"}],"generics":[{"name":"CoreGeneric"}]}`)
		case "/api/schema/check":
			_, _ = io.WriteString(w, `{"valid":true}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{name: "graphql", args: []string{"schema", "graphql"}, wantStdout: "type Query { ok: Boolean }\n"},
		{name: "export", args: []string{"schema", "export", "--namespaces", "Builtin", "--branch", "feature"}, wantStdout: `"BuiltinTag"`},
		{name: "list filtered", args: []string{"schema", "list", "--filter", "core", "--branch", "feature"}, wantStdout: `"CoreDevice"`},
		{name: "show", args: []string{"schema", "show", "BuiltinTag", "--branch", "feature"}, wantStdout: `"kind": "BuiltinTag"`},
		{name: "show missing", args: []string{"schema", "show", "Missing", "--branch", "feature"}, wantExit: 1, wantStderr: "Missing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"-branch", "feature"}, test.args...)
			stdout, stderr, exitCode := runCLI(t, server, args...)
			if exitCode != test.wantExit || !strings.Contains(stdout, test.wantStdout) || !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
		})
	}
}

func TestObjectCRUDCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		args          []string
		wantOperation string
		response      string
		wantExit      int
		wantStdout    string
		check         func(*testing.T, cliRequest)
	}{
		{name: "get by HFID", args: []string{"object", "get", "BuiltinTag", "platform/core", "--branch", "feature"}, wantOperation: "GetBuiltinTagByHFID", response: `{"data":{"BuiltinTag":{"count":1,"edges":[{"node":{"id":"tag-id","kind":"BuiltinTag","hfid":["platform","core"],"display_label":"core"}}]}}}`, wantStdout: `"id": "tag-id"`},
		{name: "query", args: []string{"object", "get", "BuiltinTag", "--filter", "name=core", "--limit", "5", "--offset", "2"}, wantOperation: "QueryBuiltinTag", response: `{"data":{"BuiltinTag":{"count":1,"edges":[{"node":{"id":"tag-id","kind":"BuiltinTag","hfid":["core"],"display_label":"core"}}]}}}`, wantStdout: `"Limit": 5`, check: func(t *testing.T, request cliRequest) {
			t.Helper()
			if request.Variables["offset"] != float64(2) || request.Variables["limit"] != float64(5) || request.Variables["filter0"] != "core" {
				t.Errorf("variables = %#v", request.Variables)
			}
		}},
		{name: "query empty", args: []string{"object", "get", "BuiltinTag"}, wantOperation: "QueryBuiltinTag", response: `{"data":{"BuiltinTag":{"count":0,"edges":[]}}}`, wantExit: 80},
		{name: "update", args: []string{"object", "update", "BuiltinTag", "platform/core", "--set", `description="updated"`, "--branch", "feature"}, wantOperation: "BuiltinTagUpdate", response: `{"data":{"BuiltinTagUpdate":{"ok":true,"object":{"id":"tag-id","kind":"BuiltinTag","hfid":["platform","core"],"display_label":"core"}}}}`, wantStdout: `"id": "tag-id"`, check: func(t *testing.T, request cliRequest) {
			t.Helper()
			data, _ := request.Variables["data"].(map[string]any)
			hfid, _ := data["hfid"].([]any)
			if len(hfid) != 2 || hfid[0] != "platform" {
				t.Errorf("variables = %#v", request.Variables)
			}
		}},
		{name: "delete", args: []string{"object", "delete", "BuiltinTag", "platform/core", "--yes", "--branch", "feature"}, wantOperation: "BuiltinTagDelete", response: `{"data":{"BuiltinTagDelete":{"ok":true}}}`, wantStdout: "delete: ok"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				payload := decodeCLIRequest(t, request)
				if payload.OperationName != test.wantOperation {
					t.Errorf("operation = %q", payload.OperationName)
				}
				if test.check != nil {
					test.check(t, payload)
				}
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()

			stdout, stderr, exitCode := runCLI(t, server, test.args...)
			if exitCode != test.wantExit || !strings.Contains(stdout, test.wantStdout) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
		})
	}
}

func TestObjectAndCommandValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "object command required", args: []string{"object"}, want: "object <get|create|update|delete>"},
		{name: "delete confirmation", args: []string{"object", "delete", "BuiltinTag", "core"}, want: "requires --yes"},
		{name: "unsupported output", args: []string{"object", "get", "--output", "table", "BuiltinTag"}, want: "only -output json"},
		{name: "invalid filter", args: []string{"object", "get", "--filter", "invalid", "BuiltinTag"}, want: "must use key=value"},
		{name: "create data required", args: []string{"object", "create", "BuiltinTag"}, want: "at least one --set"},
		{name: "mutually exclusive data", args: []string{"object", "create", "BuiltinTag", "--set", "name=x", "--file", "x.yml"}, want: "mutually exclusive"},
		{name: "negative schema wait", args: []string{"schema", "check", "--wait", "-1", "schema.yml"}, want: "--wait must not be negative"},
		{name: "invalid diff time", args: []string{"diff", "tree", "--from", "yesterday"}, want: "cannot parse"},
		{name: "objectstore selector required", args: []string{"objectstore", "file"}, want: "set exactly one"},
		{name: "objectstore kind required", args: []string{"objectstore", "file", "--hfid", "a/b"}, want: "set exactly one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			exitCode := testRunner(&stdout, &stderr).Run(t.Context(), append([]string{"-address", "https://example.com"}, test.args...))
			if exitCode != 2 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestObjectStoreCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantHFID []string
	}{
		{name: "get", args: []string{"objectstore", "get", "stored/id"}, wantPath: "/api/storage/object/stored%2Fid"},
		{name: "file by storage ID", args: []string{"objectstore", "file", "--storage-id", "storage/id"}, wantPath: "/api/storage/files/by-storage-id/storage%2Fid"},
		{name: "file by node ID", args: []string{"objectstore", "file", "--id", "node/id"}, wantPath: "/api/storage/files/node%2Fid"},
		{name: "file by HFID", args: []string{"objectstore", "file", "--kind", "CoreFile", "--hfid", "folder/file"}, wantPath: "/api/storage/files/by-hfid/CoreFile", wantHFID: []string{"folder", "file"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.EscapedPath() != test.wantPath {
					t.Errorf("path = %q", request.URL.EscapedPath())
				}
				if len(test.wantHFID) > 0 && !equalStrings(request.URL.Query()["hfid"], test.wantHFID) {
					t.Errorf("hfid = %#v", request.URL.Query()["hfid"])
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, "hello")
			}))
			defer server.Close()

			stdout, stderr, exitCode := runCLI(t, server, test.args...)
			if exitCode != 0 || stdout != "hello\n" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
		})
	}
}

func TestDiffTreeOptionsAndCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		if payload.OperationName != "GetDiffTree" || payload.Variables["name"] != "saved" || payload.Variables["fromTime"] != "2026-08-01T00:00:00Z" {
			t.Errorf("request = %#v", payload)
		}
		_, _ = io.WriteString(w, `{"data":{"DiffTree":{"name":"saved","from_time":"2026-08-01T00:00:00Z","to_time":"2026-08-02T00:00:00Z","base_branch":"main","diff_branch":"feature","num_added":1,"nodes":[]}}}`)
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "diff", "tree", "feature", "--name", "saved", "--from", "2026-08-01T00:00:00Z", "--to", "2026-08-02T00:00:00Z")
	if exitCode != 0 || !strings.Contains(stdout, `"DiffBranch": "feature"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var canceledOut, canceledErr bytes.Buffer
	exitCode = testRunner(&canceledOut, &canceledErr).Run(ctx, []string{"-address", server.URL, "branch", "list"})
	if exitCode != 1 || !strings.Contains(canceledErr.String(), "context canceled") {
		t.Fatalf("exit=%d stderr=%q", exitCode, canceledErr.String())
	}
}

func TestGraphQLRequestAndServerError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		payload := decodeCLIRequest(t, request)
		if payload.OperationName != "Lookup" || payload.Variables["id"] != "42" || !strings.Contains(payload.Query, "query Lookup") {
			t.Errorf("request = %#v", payload)
		}
		_, _ = io.WriteString(w, `{"data":{"value":42}}`)
	}))
	defer server.Close()
	stdout, stderr, exitCode := runCLI(t, server, "graphql", "--query", "query Lookup($id: ID!) { value }", "--operation", "Lookup", "--variables", `{"id":"42"}`)
	if exitCode != 0 || !strings.Contains(stdout, `"value": 42`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failure", http.StatusInternalServerError)
	}))
	defer errorServer.Close()
	_, stderr, exitCode = runCLI(t, errorServer, "branch", "list")
	if exitCode != 1 || !strings.Contains(stderr, "HTTP 500") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
	}
}

func TestOutputErrors(t *testing.T) {
	t.Parallel()
	writer := errorWriter{}
	runner := testRunner(&bytes.Buffer{}, &bytes.Buffer{})
	runner.Stdout = writer
	if exitCode := runner.Run(t.Context(), []string{"version"}); exitCode != 1 {
		t.Fatalf("version exit = %d", exitCode)
	}
	if exitCode := runner.writeJSON(map[string]string{"ok": "yes"}); exitCode != 1 {
		t.Fatalf("writeJSON exit = %d", exitCode)
	}
	if exitCode := runner.writeText("text"); exitCode != 1 {
		t.Fatalf("writeText exit = %d", exitCode)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestSchemaHelpers(t *testing.T) {
	t.Parallel()
	schema := map[string]any{
		"nodes":    []any{map[string]any{"kind": "BuiltinTag"}, "invalid"},
		"generics": []any{map[string]any{"name": "CoreGeneric"}},
	}
	filtered := filterSchemaKinds(schema, "core")
	if len(filtered) != 2 {
		t.Fatalf("filtered = %#v", filtered)
	}
	item, found := findSchemaKind(schema, "BuiltinTag")
	if !found || kindName(item) != "BuiltinTag" {
		t.Fatalf("item=%#v found=%t", item, found)
	}
	if _, found := findSchemaKind(schema, "Missing"); found {
		t.Fatal("unexpected missing kind")
	}
	if schemaCollections([]any{}) != nil || kindName("invalid") != "" {
		t.Fatal("unexpected collection or kind name")
	}
}

func TestParseHelpers(t *testing.T) {
	t.Parallel()
	assignments, err := parseAssignments([]string{"enabled=true", `count=12`, `name=tag`, `config={"x":1}`}, true)
	if err != nil {
		t.Fatal(err)
	}
	if assignments["config"].(map[string]any)["x"] != json.Number("1") {
		t.Fatalf("assignments = %#v", assignments)
	}
	if _, err := parseAssignments([]string{"invalid"}, false); err == nil {
		t.Fatal("expected invalid assignment error")
	}
	if _, err := parseOptionalTime("invalid"); err == nil {
		t.Fatal("expected invalid timestamp error")
	}
	if value, err := parseOptionalTime(""); err != nil || !value.IsZero() {
		t.Fatalf("value=%v err=%v", value, err)
	}
}
