package config_test

import (
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
