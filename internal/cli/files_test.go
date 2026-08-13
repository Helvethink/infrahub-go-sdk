package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flag "github.com/spf13/pflag"
)

func TestReadObjectDocuments(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "objects.yml")
	if err := os.WriteFile(path, []byte(`
---
apiVersion: infrahub.app/v1
kind: Object
spec:
  kind: BuiltinTag
  data:
    - name: staging
      description: Test tag
---
apiVersion: infrahub.app/v1
kind: Object
spec:
  kind: CoreStandardGroup
  data:
    - name: platform
`), 0o600); err != nil {
		t.Fatal(err)
	}
	documents, err := readObjectDocuments([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 || documents[0].Kind != "BuiltinTag" || documents[1].Kind != "CoreStandardGroup" {
		t.Fatalf("documents = %#v", documents)
	}
	data := normalizeObjectData(documents[0].Data[0])
	name, ok := data["name"].(map[string]any)
	if !ok || name["value"] != "staging" {
		t.Fatalf("normalized data = %#v", data)
	}
}

func TestObjectValidateDoesNotRequireAddress(t *testing.T) {
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
	var stdout, stderr bytes.Buffer
	exitCode := testRunner(&stdout, &stderr).Run(t.Context(), []string{"object", "validate", path})
	if exitCode != 0 || !strings.Contains(stdout.String(), `"valid": true`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
}

func TestParseInterspersedAcceptsFlagsAfterArguments(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	branch := flags.String("branch", "main", "")
	force := flags.Bool("force", false, "")
	if err := parseInterspersed(flags, []string{"objects.yml", "--branch", "feature", "--force"}); err != nil {
		t.Fatal(err)
	}
	if *branch != "feature" || !*force || flags.NArg() != 1 || flags.Arg(0) != "objects.yml" {
		t.Fatalf("branch=%q force=%t args=%v", *branch, *force, flags.Args())
	}
}
