// Package config loads Infrahub client configuration from TOML and the
// environment. It contains data only and does not construct SDK clients.
package config

import (
	"fmt"
	"io"
	"os"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	"github.com/spf13/viper"
)

const (
	EnvAddress       = "INFRAHUB_ADDRESS"
	EnvAPIToken      = "INFRAHUB_API_TOKEN"
	EnvDefaultBranch = "INFRAHUB_BRANCH"
	EnvConfigPath    = "INFRAHUB_CONFIG"
	EnvLogLevel      = "INFRAHUB_LOG_LEVEL"

	EnvDefaultBranchAlias = "INFRAHUB_DEFAULT_BRANCH"
	EnvConfigPathAlias    = "INFRAHUBCTL_CONFIG"
)

// Config contains connection settings shared by the SDK and infrahubctl.
type Config struct {
	Address       string `mapstructure:"address"`
	ServerAddress string `mapstructure:"server_address"`
	APIToken      string `mapstructure:"api_token"`
	DefaultBranch string `mapstructure:"default_branch"`
	LogLevel      string `mapstructure:"log_level"`
}

// Load reads a strict TOML configuration file from path.
func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	result, err := Decode(file)
	if err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	return result, nil
}

// Decode reads strict TOML configuration from reader.
func Decode(reader io.Reader) (Config, error) {
	loader := viper.New()
	loader.SetConfigType("toml")
	if err := loader.ReadConfig(reader); err != nil {
		return Config{}, fmt.Errorf("decode TOML: %w", err)
	}
	var result Config
	if err := loader.UnmarshalExact(&result); err != nil {
		return Config{}, fmt.Errorf("decode TOML: %w", err)
	}
	if result.Address == "" {
		result.Address = result.ServerAddress
	}
	return result, nil
}

// ApplyEnvironment returns a copy with non-empty environment variables
// applied. Environment values take precedence over file values.
func (c Config) ApplyEnvironment(getenv func(string) string) Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := getenv(EnvAddress); value != "" {
		c.Address = value
	}
	if value := getenv(EnvAPIToken); value != "" {
		c.APIToken = value
	}
	if value := getenv(EnvDefaultBranch); value != "" {
		c.DefaultBranch = value
	} else if value := getenv(EnvDefaultBranchAlias); value != "" {
		c.DefaultBranch = value
	}
	if value := getenv(EnvLogLevel); value != "" {
		c.LogLevel = value
	}
	return c
}

// NewClient constructs an SDK client from the configuration. Additional
// options are applied last and may override file or environment settings.
func (c Config) NewClient(options ...infrahub.Option) (*infrahub.Client, error) {
	baseOptions := make([]infrahub.Option, 0, len(options)+2)
	baseOptions = append(baseOptions, infrahub.WithAPIToken(c.APIToken))
	if c.DefaultBranch != "" {
		baseOptions = append(baseOptions, infrahub.WithDefaultBranch(c.DefaultBranch))
	}
	baseOptions = append(baseOptions, options...)
	return infrahub.NewClient(c.Address, baseOptions...)
}
