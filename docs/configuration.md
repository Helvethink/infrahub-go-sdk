# Configuration

The SDK can be configured directly with Go options or through the `pkg/config` package. The `infrahubctl` command uses Cobra and pflag for its command tree and Viper for configuration. It supports TOML, environment variables, and flags.

## TOML file

The default path is the operating system's user configuration directory followed by `infrahub/config.toml`:

- Linux: `${XDG_CONFIG_HOME:-$HOME/.config}/infrahub/config.toml`
- macOS: `$HOME/Library/Application Support/infrahub/config.toml`
- Windows: `%AppData%\infrahub\config.toml`

```toml
address = "https://infrahub.example.com"
api_token = "replace-me"
default_branch = "main"
log_level = "error"
```

For migration from the Python `infrahubctl`, `server_address` is accepted as an alias for `address`.

Unknown TOML keys are rejected so configuration typos fail early. When the file contains an API token, restrict its permissions to the current user.

Select another file with either:

```sh
infrahubctl -config ./infrahub.toml branch list
export INFRAHUB_CONFIG=./infrahub.toml
export INFRAHUBCTL_CONFIG=./infrahub.toml
```

An explicitly selected file must exist. The default file is optional.

## Environment variables

```sh
export INFRAHUB_ADDRESS=https://infrahub.example.com
export INFRAHUB_API_TOKEN=replace-me
export INFRAHUB_BRANCH=main
export INFRAHUB_DEFAULT_BRANCH=main
export INFRAHUB_LOG_LEVEL=error
```

Environment variables override values from TOML. Explicit CLI flags override both:

```text
flags > environment > TOML > defaults
```

Prefer the environment or a protected TOML file for the token; command-line token flags can be visible in process listings and shell history.

## Logging

The CLI writes command results to stdout and zap diagnostics to stderr as JSON. The default level is `error`; use `debug`, `info`, `warn`, `error`, or `off` through `--log-level`, `INFRAHUB_LOG_LEVEL`, or `log_level` in TOML. For example:

```sh
infrahubctl --log-level info branch list
```

Log events contain the command name, exit code, duration, and safe error details. Command arguments, GraphQL variables, API tokens, and authorization headers are not logged. With `off`, operational errors remain available as plain stderr diagnostics.

## Go applications

```go
import "github.com/Helvethink/infrahub-go-sdk/pkg/config"

settings, err := config.Load("infrahub.toml")
if err != nil {
    return err
}
settings = settings.ApplyEnvironment(os.Getenv)

client, err := settings.NewClient()
```

Applications that do not need file-based configuration can continue using `infrahub.NewClient` and functional options directly.

## Implementation dependency

Configuration loading uses Viper because the Go standard library does not parse TOML and Viper provides an extensible configuration boundary for the CLI. Each load uses an isolated Viper instance; package-global Viper state is not used. Cobra supplies the nested command tree, pflag supplies repeatable and interspersed flags, and zap supplies structured logging. The dependencies are pinned in `go.mod`; Viper and zap use the MIT license and Cobra uses Apache-2.0. Their additional transitive cost is accepted for the command configuration, parsing, and logging layer.
