package main

import (
	"os"
	"testing"
)

func TestRunVersion(t *testing.T) {
	originalArgs := os.Args
	originalVersion, originalCommit, originalDate := version, commit, date
	t.Cleanup(func() {
		os.Args = originalArgs
		version, commit, date = originalVersion, originalCommit, originalDate
	})
	os.Args = []string{"infrahubctl", "version"}
	version, commit, date = "1.2.3", "abc123", "2026-08-17"
	if code := run(); code != 0 {
		t.Fatalf("run() = %d", code)
	}
}
