package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Helvethink/infrahub-go-sdk/pkg/config"
)

func TestDecode(t *testing.T) {
	t.Parallel()
	result, err := config.Decode(strings.NewReader(`
address = "https://infrahub.example.com"
api_token = "secret"
default_branch = "develop"
log_level = "info"
`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "https://infrahub.example.com" || result.APIToken != "secret" || result.DefaultBranch != "develop" || result.LogLevel != "info" {
		t.Fatalf("config = %#v", result)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	if _, err := config.Decode(strings.NewReader(`unknown_key = "typo"`)); err == nil {
		t.Fatal("Decode() error = nil")
	}
}

func TestEnvironmentOverridesFileValues(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		config.EnvAddress:       "https://environment.example.com",
		config.EnvDefaultBranch: "environment",
		config.EnvLogLevel:      "debug",
	}
	result := (config.Config{
		Address: "https://file.example.com", APIToken: "file-secret", DefaultBranch: "file",
	}).ApplyEnvironment(func(name string) string { return values[name] })
	if result.Address != values[config.EnvAddress] || result.DefaultBranch != "environment" || result.APIToken != "file-secret" || result.LogLevel != "debug" {
		t.Fatalf("config = %#v", result)
	}
}

func TestPythonCLICompatibilityConfig(t *testing.T) {
	t.Parallel()
	result, err := config.Decode(strings.NewReader(`server_address = "https://python.example.com"`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "https://python.example.com" {
		t.Fatalf("Address = %q", result.Address)
	}
	result = result.ApplyEnvironment(func(name string) string {
		if name == config.EnvDefaultBranchAlias {
			return "python-env"
		}
		return ""
	})
	if result.DefaultBranch != "python-env" {
		t.Fatalf("DefaultBranch = %q", result.DefaultBranch)
	}
}

func TestAddressTakesPrecedenceOverCompatibilityAlias(t *testing.T) {
	t.Parallel()
	result, err := config.Decode(strings.NewReader(`
address = "https://current.example.com"
server_address = "https://legacy.example.com"
`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "https://current.example.com" {
		t.Fatalf("Address = %q", result.Address)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`address = "https://infrahub.example.com"
default_branch = "develop"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != "https://infrahub.example.com" || result.DefaultBranch != "develop" {
		t.Fatalf("config = %#v", result)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	if _, err := config.Load(filepath.Join(t.TempDir(), "missing.toml")); err == nil || !strings.Contains(err.Error(), "open config") {
		t.Fatalf("missing file error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(path, []byte(`address = [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "decode config") {
		t.Fatalf("invalid file error = %v", err)
	}
}

func TestDecodeInvalidTOML(t *testing.T) {
	t.Parallel()
	if _, err := config.Decode(strings.NewReader(`address = [`)); err == nil || !strings.Contains(err.Error(), "decode TOML") {
		t.Fatalf("error = %v", err)
	}
}

func TestEnvironmentTokenAndFallback(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		config.EnvAPIToken:           "environment-secret",
		config.EnvDefaultBranchAlias: "legacy-branch",
	}
	result := (config.Config{DefaultBranch: "file"}).ApplyEnvironment(func(name string) string { return values[name] })
	if result.APIToken != "environment-secret" || result.DefaultBranch != "legacy-branch" {
		t.Fatalf("config = %#v", result)
	}
}

func TestApplyEnvironmentUsesProcessEnvironment(t *testing.T) {
	t.Setenv(config.EnvAddress, "https://environment.example.com")
	t.Setenv(config.EnvAPIToken, "environment-secret")
	result := (config.Config{}).ApplyEnvironment(nil)
	if result.Address != "https://environment.example.com" || result.APIToken != "environment-secret" {
		t.Fatalf("config = %#v", result)
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()
	client, err := (config.Config{Address: "https://infrahub.example.com", DefaultBranch: "develop"}).NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if client.DefaultBranch() != "develop" {
		t.Fatalf("default branch = %q", client.DefaultBranch())
	}
	if _, err := (config.Config{Address: "/relative"}).NewClient(); err == nil {
		t.Fatal("NewClient() error = nil")
	}
}
