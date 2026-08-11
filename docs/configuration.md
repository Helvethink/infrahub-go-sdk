# Configuration

The SDK can be configured directly with Go options or through the `pkg/config` package. The `infrahubctl` command supports TOML, environment variables, and flags.

## TOML file

The default path is the operating system's user configuration directory followed by `infrahub/config.toml`:

- Linux: `${XDG_CONFIG_HOME:-$HOME/.config}/infrahub/config.toml`
- macOS: `$HOME/Library/Application Support/infrahub/config.toml`
- Windows: `%AppData%\infrahub\config.toml`

```toml
address = "https://infrahub.example.com"
api_token = "replace-me"
default_branch = "main"
```

Unknown TOML keys are rejected so configuration typos fail early. When the file contains an API token, restrict its permissions to the current user.

Select another file with either:

```sh
infrahubctl -config ./infrahub.toml branch list
export INFRAHUB_CONFIG=./infrahub.toml
```

An explicitly selected file must exist. The default file is optional.

## Environment variables

```sh
export INFRAHUB_ADDRESS=https://infrahub.example.com
export INFRAHUB_API_TOKEN=replace-me
export INFRAHUB_BRANCH=main
```

Environment variables override values from TOML. Explicit CLI flags override both:

```text
flags > environment > TOML > defaults
```

Prefer the environment or a protected TOML file for the token; command-line token flags can be visible in process listings and shell history.

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
