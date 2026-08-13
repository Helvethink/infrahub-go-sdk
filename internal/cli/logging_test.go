package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfoLoggingIsStructured(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(t.Context(), []string{"--log-level", "info", "version"})
	if exitCode != 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
	events := decodeLogEvents(t, stderr.String())
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[0]["msg"] != "command started" || events[0]["command"] != "infrahubctl version" {
		t.Fatalf("started event = %#v", events[0])
	}
	if events[1]["msg"] != "command finished" || events[1]["exit_code"] != float64(0) {
		t.Fatalf("finished event = %#v", events[1])
	}
}

func TestLoggingRedactsTokenAndKeepsStdoutClean(t *testing.T) {
	t.Parallel()
	const token = "super-secret-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	stdout, stderr, exitCode := runCLI(t, server, "--log-level", "debug", "--token", token, "branch", "list")
	if exitCode != 1 || stdout != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if strings.Contains(stderr, token) || strings.Contains(stderr, "X-INFRAHUB-KEY") || strings.Contains(stderr, "Authorization") {
		t.Fatalf("secret leaked in logs: %q", stderr)
	}
	events := decodeLogEvents(t, stderr)
	if len(events) < 3 || events[1]["level"] != "error" || !strings.Contains(events[1]["error"].(string), "HTTP 500") {
		t.Fatalf("events = %#v", events)
	}
}

func TestLogLevelValidationAndOff(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(t.Context(), []string{"--log-level", "verbose", "version"})
	if exitCode != 2 || !strings.Contains(stderr.String(), "invalid log level") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	stdout.Reset()
	stderr.Reset()
	exitCode = testRunner(&stdout, &stderr).Run(t.Context(), []string{"--address", server.URL, "--log-level", "off", "branch", "list"})
	if exitCode != 1 || !strings.HasPrefix(stderr.String(), "infrahubctl:") || strings.Contains(stderr.String(), `"level"`) {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}

func decodeLogEvents(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode log %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}
