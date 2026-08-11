package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRunner(stdout, stderr *bytes.Buffer) Runner {
	return Runner{
		Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr,
		Getenv:        func(string) string { return "" },
		UserConfigDir: func() (string, error) { return "/path/that/does/not/exist", nil },
		Build:         BuildInfo{Version: "1.2.3", Commit: "abc123", Date: "2026-08-11"},
	}
}

func TestConfigPrecedence(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(path, []byte(`
address = "https://file.example.com"
api_token = "file-token"
default_branch = "file"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/graphql/flag" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		if r.Header.Get("X-INFRAHUB-KEY") != "environment-token" {
			t.Errorf("token = %q", r.Header.Get("X-INFRAHUB-KEY"))
		}
		_, _ = w.Write([]byte(`{"data":{"Branch":[]}}`))
	}))
	defer server.Close()
	values := map[string]string{
		"INFRAHUB_ADDRESS":   server.URL,
		"INFRAHUB_API_TOKEN": "environment-token",
		"INFRAHUB_BRANCH":    "environment",
	}
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	runner.Getenv = func(name string) string { return values[name] }
	exitCode := runner.Run(context.Background(), []string{"-config", path, "-branch", "flag", "branch", "list"})
	if exitCode != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	configDirectory := filepath.Join(directory, "infrahub")
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDirectory, "config.toml"), []byte(`address = "https://config.example.com"`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	runner.UserConfigDir = func() (string, error) { return directory, nil }
	// Invalid variables are rejected after the client has been created, proving
	// that the address was loaded from the default configuration file.
	exitCode := runner.Run(context.Background(), []string{"graphql", "-query", "query { x }", "-variables", "invalid"})
	if exitCode != 2 || !strings.Contains(stderr.String(), "invalid --variables") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestExplicitMissingConfigFails(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-config", filepath.Join(t.TempDir(), "missing.toml"), "branch", "list",
	})
	if exitCode != 1 || !strings.Contains(stderr.String(), "open config") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestVersionDoesNotRequireAddress(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{"version"})
	if exitCode != 0 || stdout.String() != "infrahubctl 1.2.3 (abc123) built 2026-08-11\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestMissingAddressIsUsageError(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{"branch", "list"})
	if exitCode != 2 || !strings.Contains(stderr.String(), "INFRAHUB_ADDRESS") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestHelpDoesNotRequireAddress(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{"help"})
	if exitCode != 0 || !strings.Contains(stderr.String(), "branch list") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestBranchList(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/graphql/main" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"data":{"Branch":[{"id":"1","name":"main","branched_from":"x","is_default":true,"sync_with_git":false,"has_schema_changes":false,"status":"OPEN"}]}}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{"-address", server.URL, "branch", "list"})
	if exitCode != 0 || !strings.Contains(stdout.String(), `"name": "main"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestGraphQLReadsStdin(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"value":42}}`))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	runner := testRunner(&stdout, &stderr)
	runner.Stdin = strings.NewReader("query { value }")
	exitCode := runner.Run(context.Background(), []string{"-address", server.URL, "graphql"})
	if exitCode != 0 || !strings.Contains(stdout.String(), `"value": 42`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestInvalidVariablesIsUsageError(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(context.Background(), []string{
		"-address", "https://example.com", "graphql", "-query", "query { x }", "-variables", "not-json",
	})
	if exitCode != 2 || !strings.Contains(stderr.String(), "invalid --variables") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}
