package cli

import (
	"context"
	"strings"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	sdkconfig "github.com/Helvethink/infrahub-go-sdk/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const offlineCommand = "offline"

type commandState struct {
	runner Runner
	ctx    context.Context

	configPath string
	address    string
	token      string
	branch     string

	settings sdkconfig.Config
	client   *infrahub.Client
}

type exitStatusError struct{ code int }

func (e *exitStatusError) Error() string { return "" }

func newRootCommand(ctx context.Context, runner Runner) *cobra.Command {
	state := &commandState{runner: runner, ctx: ctx}
	root := &cobra.Command{
		Use:           "infrahubctl",
		Short:         "Command-line client for Infrahub",
		Long:          "Command-line client for Infrahub.\n\nRun commands such as branch list, object get, schema graphql, task list, or graphql.",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			runner.printUsage()
			return statusError(2)
		},
		PersistentPreRunE: state.prepareClient,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetContext(ctx)
	root.SetIn(runner.Stdin)
	root.SetOut(runner.Stderr)
	root.SetErr(runner.Stderr)
	root.SetHelpFunc(func(*cobra.Command, []string) { runner.printUsage() })
	root.PersistentFlags().StringVar(&state.configPath, "config", "", "TOML config file")
	root.PersistentFlags().StringVar(&state.address, "address", "", "Infrahub base URL")
	root.PersistentFlags().StringVar(&state.token, "token", "", "Infrahub API token")
	root.PersistentFlags().StringVar(&state.branch, "branch", "", "Infrahub branch")

	root.AddCommand(
		state.versionCommand(),
		state.infoCommand(),
		state.branchCommand(),
		state.diffCommand(),
		state.objectCommand(),
		state.objectStoreCommand(),
		state.repositoryCommand(),
		state.schemaCommand(),
		state.taskCommand(),
		state.graphQLCommand(),
	)
	return root
}

func (s *commandState) prepareClient(command *cobra.Command, args []string) error {
	if command == command.Root() || command.Name() == "help" || command.Annotations[offlineCommand] == "true" {
		return nil
	}
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return nil
		}
	}
	settings, err := s.runner.loadConfig(s.configPath, command.Root().PersistentFlags().Changed("config"))
	if err != nil {
		return statusError(s.runner.fail(err))
	}

	loader := viper.New()
	loader.SetDefault("default_branch", "main")
	if err := loader.MergeConfigMap(configMap(settings)); err != nil {
		return statusError(s.runner.fail(err))
	}
	if err := loader.MergeConfigMap(environmentMap(s.runner.Getenv)); err != nil {
		return statusError(s.runner.fail(err))
	}
	flags := command.Root().PersistentFlags()
	for key, name := range map[string]string{
		"address": "address", "api_token": "token", "default_branch": "branch",
	} {
		if err := loader.BindPFlag(key, flags.Lookup(name)); err != nil {
			return statusError(s.runner.fail(err))
		}
	}
	if flags.Changed("address") {
		loader.Set("address", s.address)
	}
	if flags.Changed("token") {
		loader.Set("api_token", s.token)
	}
	if flags.Changed("branch") {
		loader.Set("default_branch", s.branch)
	}
	s.settings = sdkconfig.Config{
		Address:       loader.GetString("address"),
		APIToken:      loader.GetString("api_token"),
		DefaultBranch: loader.GetString("default_branch"),
	}
	if s.settings.Address == "" {
		return statusError(s.runner.usageError("infrahubctl: address is required in --address, INFRAHUB_ADDRESS, or the config file"))
	}
	s.client, err = s.settings.NewClient()
	if err != nil {
		return statusError(s.runner.fail(err))
	}
	return nil
}

func normalizeNamedFlags(args []string, names map[string]struct{}) []string {
	result := append([]string(nil), args...)
	for index, arg := range result {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") || arg == "-" {
			continue
		}
		name := strings.TrimPrefix(arg, "-")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		if _, found := names[name]; found {
			result[index] = "-" + arg
		}
	}
	return result
}

func configMap(settings sdkconfig.Config) map[string]any {
	result := map[string]any{}
	if settings.Address != "" {
		result["address"] = settings.Address
	}
	if settings.APIToken != "" {
		result["api_token"] = settings.APIToken
	}
	if settings.DefaultBranch != "" {
		result["default_branch"] = settings.DefaultBranch
	}
	return result
}

func environmentMap(getenv func(string) string) map[string]any {
	result := map[string]any{}
	if value := getenv(sdkconfig.EnvAddress); value != "" {
		result["address"] = value
	}
	if value := getenv(sdkconfig.EnvAPIToken); value != "" {
		result["api_token"] = value
	}
	if value := getenv(sdkconfig.EnvDefaultBranch); value != "" {
		result["default_branch"] = value
	} else if value := getenv(sdkconfig.EnvDefaultBranchAlias); value != "" {
		result["default_branch"] = value
	}
	return result
}

func statusError(code int) error {
	if code == 0 {
		return nil
	}
	return &exitStatusError{code: code}
}

func (s *commandState) leaf(use string, run func([]string) int) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		DisableFlagParsing: true,
		RunE: func(command *cobra.Command, args []string) error {
			for _, arg := range args {
				if arg == "-h" || arg == "--help" {
					return command.Help()
				}
			}
			return statusError(run(args))
		},
	}
}

func (s *commandState) group(use string, run func([]string) int, leaves ...string) *cobra.Command {
	command := &cobra.Command{
		Use: use,
		RunE: func(*cobra.Command, []string) error {
			return statusError(run(nil))
		},
	}
	for _, leaf := range leaves {
		name := leaf
		command.AddCommand(s.leaf(name, func(args []string) int {
			return run(append([]string{name}, args...))
		}))
	}
	return command
}

func (s *commandState) versionCommand() *cobra.Command {
	command := s.leaf("version", func([]string) int { return s.runner.runVersion() })
	command.Annotations = map[string]string{offlineCommand: "true"}
	return command
}

func (s *commandState) infoCommand() *cobra.Command {
	return s.leaf("info", func([]string) int { return s.runner.runInfo(s.client) })
}

func (s *commandState) branchCommand() *cobra.Command {
	return s.group("branch", func(args []string) int {
		return s.runner.runBranch(s.ctx, s.client, args)
	}, "list", "get", "create", "delete", "rebase", "validate", "merge", "report")
}

func (s *commandState) diffCommand() *cobra.Command {
	return s.group("diff", func(args []string) int {
		return s.runner.runDiff(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "tree", "summary")
}

func (s *commandState) objectCommand() *cobra.Command {
	command := s.group("object", func(args []string) int {
		return s.runner.runObject(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "get", "create", "update", "delete", "load")
	validate := s.leaf("validate", s.runner.runObjectValidate)
	validate.Annotations = map[string]string{offlineCommand: "true"}
	command.AddCommand(validate)
	return command
}

func (s *commandState) objectStoreCommand() *cobra.Command {
	return s.group("objectstore", func(args []string) int {
		return s.runner.runObjectStore(s.ctx, s.client, args)
	}, "get", "upload", "file")
}

func (s *commandState) repositoryCommand() *cobra.Command {
	return s.group("repository", func(args []string) int {
		return s.runner.runRepository(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "list")
}

func (s *commandState) schemaCommand() *cobra.Command {
	return s.group("schema", func(args []string) int {
		return s.runner.runSchema(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "graphql", "load", "check", "export", "list", "show")
}

func (s *commandState) taskCommand() *cobra.Command {
	return s.group("task", func(args []string) int {
		return s.runner.runTask(s.ctx, s.client, args)
	}, "list")
}

func (s *commandState) graphQLCommand() *cobra.Command {
	return s.leaf("graphql", func(args []string) int {
		return s.runner.runGraphQL(s.ctx, s.client, s.settings.DefaultBranch, args)
	})
}
