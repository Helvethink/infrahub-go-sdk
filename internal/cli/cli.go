// Package cli implements the infrahubctl command without coupling command
// parsing to the executable entry point.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	sdkconfig "github.com/Helvethink/infrahub-go-sdk/pkg/config"
	flag "github.com/spf13/pflag"
	"go.uber.org/zap"
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
	// Logger receives structured CLI diagnostics. When nil, Runner creates a
	// JSON logger writing to Stderr at the configured log level.
	Logger         *zap.Logger
	loggerInjected bool
	logErrors      bool
	// UserConfigDir returns the base directory used for the optional default
	// configuration file. It defaults to os.UserConfigDir.
	UserConfigDir func() (string, error)
	Build         BuildInfo
}

// Run executes the CLI and returns a process exit code.
func (r Runner) Run(ctx context.Context, args []string) int {
	r = r.withDefaults()
	command := newRootCommand(ctx, r)
	normalized := normalizeNamedFlags(args, map[string]struct{}{
		"address": {}, "branch": {}, "config": {}, "log-level": {}, "token": {},
	})
	command.PersistentFlags().SetInterspersed(false)
	if err := command.PersistentFlags().Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.printUsage()
			return 0
		}
		_, _ = fmt.Fprintln(r.Stderr, "infrahubctl:", err)
		return 2
	}
	command.SetArgs(command.PersistentFlags().Args())
	if err := command.Execute(); err != nil {
		var status *exitStatusError
		if errors.As(err, &status) {
			return status.code
		}
		_, _ = fmt.Fprintln(r.Stderr, "infrahubctl:", err)
		return 2
	}
	return 0
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
		return r.usageError("usage: infrahubctl [global flags] branch <list|get|create|delete|rebase|validate|merge|report>")
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
		if err := parseInterspersed(command, args[1:]); err != nil {
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
	case "report":
		command := flag.NewFlagSet("branch report", flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		updateDiff := command.Bool("update-diff", false, "accepted for compatibility; reports current diff data")
		if err := parseInterspersed(command, args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() != 1 {
			return r.usageError("usage: infrahubctl branch report [flags] <name>")
		}
		if *updateDiff {
			_, _ = fmt.Fprintln(r.Stderr, "infrahubctl: --update-diff is accepted for compatibility; the Go SDK reports current diff data")
		}
		var result any
		if err := client.Branches.DiffData(ctx, command.Arg(0), false, "", "", &result); err != nil {
			return r.fail(err)
		}
		return r.writeJSON(result)
	default:
		return r.usageError("infrahubctl: unknown branch command " + args[0])
	}
}

func (r Runner) runSchema(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] schema <graphql|load|check|export|list|show>")
	}
	switch args[0] {
	case "graphql":
		if len(args) != 1 {
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
	case "load", "check":
		return r.runSchemaApply(ctx, client, branch, args[0], args[1:])
	case "export":
		return r.runSchemaExport(ctx, client, branch, args[1:])
	case "list":
		return r.runSchemaList(ctx, client, branch, args[1:])
	case "show":
		return r.runSchemaShow(ctx, client, branch, args[1:])
	default:
		return r.usageError("infrahubctl: unknown schema command " + args[0])
	}
}

func (r Runner) runInfo(client *infrahub.Client) int {
	return r.writeJSON(map[string]string{
		"client":         "infrahub-go-sdk",
		"version":        envOrValue(r.Build.Version, "dev"),
		"commit":         r.Build.Commit,
		"date":           r.Build.Date,
		"default_branch": client.DefaultBranch(),
	})
}

func (r Runner) runSchemaApply(ctx context.Context, client *infrahub.Client, branch, operation string, args []string) int {
	command := flag.NewFlagSet("schema "+operation, flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	commandBranch := command.String("branch", branch, "target branch")
	_ = command.Bool("debug", false, "accepted for compatibility")
	wait := command.Int("wait", 0, "accepted for compatibility")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() == 0 {
		return r.usageError("usage: infrahubctl schema " + operation + " [flags] <schema-file-or-directory>...")
	}
	if *wait < 0 {
		return r.usageError("--wait must not be negative")
	}
	schemas, err := readSchemaDocuments(command.Args())
	if err != nil {
		return r.fail(err)
	}
	var result any
	if operation == "load" {
		err = client.Schema.Load(ctx, *commandBranch, schemas, &result)
	} else {
		err = client.Schema.Check(ctx, *commandBranch, schemas, &result)
	}
	if err != nil {
		return r.fail(err)
	}
	return r.writeJSON(result)
}

func (r Runner) runSchemaExport(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("schema export", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	commandBranch := command.String("branch", branch, "source branch")
	namespaces := multiFlag{}
	command.Var(&namespaces, "namespaces", "namespace to export")
	_ = command.String("directory", "", "accepted for compatibility; JSON is written to stdout")
	_ = command.Bool("debug", false, "accepted for compatibility")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 0 {
		return r.usageError("usage: infrahubctl schema export [flags]")
	}
	var result any
	if err := client.Schema.Fetch(ctx, *commandBranch, namespaces, &result); err != nil {
		return r.fail(err)
	}
	return r.writeJSON(result)
}

func (r Runner) runSchemaList(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("schema list", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	filter := command.String("filter", "", "filter kinds by name")
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 0 {
		return r.usageError("usage: infrahubctl schema list [flags]")
	}
	var result any
	if err := client.Schema.Fetch(ctx, *commandBranch, nil, &result); err != nil {
		return r.fail(err)
	}
	if *filter == "" {
		return r.writeJSON(result)
	}
	return r.writeJSON(filterSchemaKinds(result, *filter))
}

func (r Runner) runSchemaShow(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("schema show", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 1 {
		return r.usageError("usage: infrahubctl schema show [flags] <kind>")
	}
	var result any
	if err := client.Schema.Fetch(ctx, *commandBranch, nil, &result); err != nil {
		return r.fail(err)
	}
	item, ok := findSchemaKind(result, command.Arg(0))
	if !ok {
		return r.fail(fmt.Errorf("schema kind %q not found", command.Arg(0)))
	}
	return r.writeJSON(item)
}

func (r Runner) runGraphQL(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("graphql", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	query := command.String("query", "", "GraphQL document; read stdin when empty")
	variables := command.String("variables", "{}", "GraphQL variables as JSON")
	operation := command.String("operation", "", "GraphQL operation name")
	if err := parseInterspersed(command, args); err != nil {
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
	if !r.logErrors {
		_, _ = fmt.Fprintln(r.Stderr, "infrahubctl:", err)
		return 1
	}
	logger := r.Logger
	if logger == nil {
		logger = newCLILogger(r.Stderr, zap.ErrorLevel)
	}
	logger.Error("command failed", zap.Error(err))
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
  info                                 print client information
  branch list                         list branches
  branch get <name>                   retrieve a branch
  branch create [flags] <name>        create a branch
  branch delete|rebase|validate|merge <name>
  branch report <name>                print branch diff report data
  diff tree|summary <branch>          inspect branch diffs
  object get|create|update|delete     manage schema-defined objects
  object load|validate                load or validate object files
  objectstore get|upload|file         manage stored content
  repository list                     list repositories
  schema graphql                      print the branch GraphQL schema
  schema load|check|export|list|show  manage schemas
  task list                           list background tasks
  graphql [flags]                     execute a GraphQL document

global flags:
  -config string    TOML file (INFRAHUB_CONFIG, INFRAHUBCTL_CONFIG, default user config directory)
  -address string   Infrahub URL (INFRAHUB_ADDRESS)
  -token string     API token (INFRAHUB_API_TOKEN)
  -branch string    branch (INFRAHUB_BRANCH, INFRAHUB_DEFAULT_BRANCH, default main)
  -log-level string logging level (INFRAHUB_LOG_LEVEL, default error)`)
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
	if r.Logger == nil {
		r.Logger = newCLILogger(r.Stderr, zap.ErrorLevel)
		r.logErrors = true
	} else {
		r.loggerInjected = true
		r.logErrors = true
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
		if path == "" {
			path = r.Getenv(sdkconfig.EnvConfigPathAlias)
		}
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
