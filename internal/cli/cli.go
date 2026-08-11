// Package cli implements the infrahubctl command without coupling command
// parsing to the executable entry point.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	sdkconfig "github.com/Helvethink/infrahub-go-sdk/config"
)

// BuildInfo contains values normally injected through linker flags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Runner owns the process-independent inputs and outputs of the CLI.
type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// UserConfigDir returns the base directory used for the optional default
	// configuration file. It defaults to os.UserConfigDir.
	UserConfigDir func() (string, error)
	Build         BuildInfo
}

// Run executes the CLI and returns a process exit code.
func (r Runner) Run(ctx context.Context, args []string) int {
	r = r.withDefaults()
	global := flag.NewFlagSet("infrahubctl", flag.ContinueOnError)
	global.SetOutput(r.Stderr)
	configPath := global.String("config", "", "TOML config file")
	address := global.String("address", "", "Infrahub base URL")
	token := global.String("token", "", "Infrahub API token")
	branch := global.String("branch", "", "Infrahub branch")
	global.Usage = func() { r.printUsage() }
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	remaining := global.Args()
	if len(remaining) == 0 {
		r.printUsage()
		return 2
	}
	if remaining[0] == "version" {
		return r.runVersion()
	}
	if remaining[0] == "help" {
		r.printUsage()
		return 0
	}
	flagsSet := make(map[string]bool)
	global.Visit(func(item *flag.Flag) { flagsSet[item.Name] = true })
	settings, err := r.loadConfig(*configPath, flagsSet["config"])
	if err != nil {
		return r.fail(err)
	}
	settings = settings.ApplyEnvironment(r.Getenv)
	if flagsSet["address"] {
		settings.Address = *address
	}
	if flagsSet["token"] {
		settings.APIToken = *token
	}
	if flagsSet["branch"] {
		settings.DefaultBranch = *branch
	}
	if settings.DefaultBranch == "" {
		settings.DefaultBranch = "main"
	}
	if settings.Address == "" {
		return r.usageError("infrahubctl: address is required in --address, INFRAHUB_ADDRESS, or the config file")
	}
	client, err := settings.NewClient()
	if err != nil {
		return r.fail(err)
	}
	switch remaining[0] {
	case "branch":
		return r.runBranch(ctx, client, remaining[1:])
	case "schema":
		return r.runSchema(ctx, client, settings.DefaultBranch, remaining[1:])
	case "graphql":
		return r.runGraphQL(ctx, client, settings.DefaultBranch, remaining[1:])
	default:
		_, _ = fmt.Fprintf(r.Stderr, "infrahubctl: unknown command %q\n", remaining[0])
		r.printUsage()
		return 2
	}
}

func (r Runner) runVersion() int {
	version := envOrValue(r.Build.Version, "dev")
	output := "infrahubctl " + version
	if r.Build.Commit != "" {
		output += " (" + r.Build.Commit + ")"
	}
	if r.Build.Date != "" {
		output += " built " + r.Build.Date
	}
	if _, err := io.WriteString(r.Stdout, output+"\n"); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}

func (r Runner) runBranch(ctx context.Context, client *infrahub.Client, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] branch <list|get|create|delete|rebase|validate|merge>")
	}
	switch args[0] {
	case "list":
		branches, err := client.Branches.List(ctx)
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(branches)
	case "get":
		if len(args) != 2 {
			return r.usageError("usage: infrahubctl branch get <name>")
		}
		result, err := client.Branches.Get(ctx, args[1])
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(result)
	case "create":
		command := flag.NewFlagSet("branch create", flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		description := command.String("description", "", "branch description")
		syncWithGit := command.Bool("sync-with-git", false, "synchronize branch with Git")
		if err := command.Parse(args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() != 1 {
			return r.usageError("usage: infrahubctl branch create [flags] <name>")
		}
		result, err := client.Branches.Create(ctx, command.Arg(0), infrahub.BranchCreateOptions{
			Description: *description, SyncWithGit: *syncWithGit,
		})
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(result)
	case "delete", "rebase", "validate", "merge":
		if len(args) != 2 {
			return r.usageError("usage: infrahubctl branch " + args[0] + " <name>")
		}
		operations := map[string]func(context.Context, string) error{
			"delete": client.Branches.Delete, "rebase": client.Branches.Rebase,
			"validate": client.Branches.Validate, "merge": client.Branches.Merge,
		}
		if err := operations[args[0]](ctx, args[1]); err != nil {
			return r.fail(err)
		}
		if _, err := fmt.Fprintf(r.Stdout, "%s: ok\n", args[0]); err != nil {
			return r.fail(fmt.Errorf("write output: %w", err))
		}
		return 0
	default:
		return r.usageError("infrahubctl: unknown branch command " + args[0])
	}
}

func (r Runner) runSchema(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) != 1 || args[0] != "graphql" {
		return r.usageError("usage: infrahubctl [global flags] schema graphql")
	}
	result, err := client.Schema.GraphQL(ctx, branch)
	if err != nil {
		return r.fail(err)
	}
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if _, err := io.WriteString(r.Stdout, result); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}

func (r Runner) runGraphQL(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("graphql", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	query := command.String("query", "", "GraphQL document; read stdin when empty")
	variables := command.String("variables", "{}", "GraphQL variables as JSON")
	operation := command.String("operation", "", "GraphQL operation name")
	if err := command.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 0 {
		return r.usageError("usage: infrahubctl [global flags] graphql [flags]")
	}
	if *query == "" {
		data, err := io.ReadAll(r.Stdin)
		if err != nil {
			return r.fail(fmt.Errorf("read GraphQL query: %w", err))
		}
		*query = string(data)
	}
	var vars map[string]any
	if err := json.Unmarshal([]byte(*variables), &vars); err != nil {
		return r.usageError("invalid --variables JSON: " + err.Error())
	}
	var result json.RawMessage
	err := client.Execute(ctx, infrahub.GraphQLRequest{
		Query: *query, Variables: vars, OperationName: *operation, Branch: branch,
	}, &result)
	if err != nil {
		return r.fail(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, result, "", "  "); err != nil {
		return r.fail(fmt.Errorf("format GraphQL response: %w", err))
	}
	if _, err := fmt.Fprintln(r.Stdout, pretty.String()); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}

func (r Runner) writeJSON(value any) int {
	encoder := json.NewEncoder(r.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}

func (r Runner) fail(err error) int {
	_, _ = fmt.Fprintln(r.Stderr, "infrahubctl:", err)
	return 1
}

func (r Runner) usageError(message string) int {
	_, _ = fmt.Fprintln(r.Stderr, message)
	return 2
}

func (r Runner) printUsage() {
	_, _ = fmt.Fprintln(r.Stderr, `usage: infrahubctl [global flags] <command>

commands:
  version                              print build information
  branch list                         list branches
  branch get <name>                   retrieve a branch
  branch create [flags] <name>        create a branch
  branch delete|rebase|validate|merge <name>
  schema graphql                      print the branch GraphQL schema
  graphql [flags]                     execute a GraphQL document

global flags:
  -config string    TOML file (INFRAHUB_CONFIG, default user config directory)
  -address string   Infrahub URL (INFRAHUB_ADDRESS)
  -token string     API token (INFRAHUB_API_TOKEN)
  -branch string    branch (INFRAHUB_BRANCH, default main)`)
}

func (r Runner) withDefaults() Runner {
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Stdout == nil {
		r.Stdout = os.Stdout
	}
	if r.Stderr == nil {
		r.Stderr = os.Stderr
	}
	if r.Getenv == nil {
		r.Getenv = os.Getenv
	}
	if r.UserConfigDir == nil {
		r.UserConfigDir = os.UserConfigDir
	}
	return r
}

func (r Runner) loadConfig(path string, explicitlySet bool) (sdkconfig.Config, error) {
	if explicitlySet && path == "" {
		return sdkconfig.Config{}, fmt.Errorf("config path must not be empty")
	}
	required := explicitlySet
	if !required {
		path = r.Getenv(sdkconfig.EnvConfigPath)
		required = path != ""
	}
	if path == "" {
		directory, err := r.UserConfigDir()
		if err != nil {
			return sdkconfig.Config{}, nil
		}
		path = filepath.Join(directory, "infrahub", "config.toml")
	}
	result, err := sdkconfig.Load(path)
	if err != nil {
		if !required && errors.Is(err, os.ErrNotExist) {
			return sdkconfig.Config{}, nil
		}
		return sdkconfig.Config{}, err
	}
	return result, nil
}

func envOrValue(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func flagExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}
