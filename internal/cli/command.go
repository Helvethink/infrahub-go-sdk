package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	sdkconfig "github.com/Helvethink/infrahub-go-sdk/pkg/config"
)

const (
	offlineCommand        = "offline"
	optionalClientCommand = "optional-client"
)

type commandState struct {
	runner Runner
	ctx    context.Context

	configPath string
	address    string
	token      string
	branch     string
	logLevel   string

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
	root.PersistentFlags().StringVar(&state.logLevel, "log-level", "", "log level: debug, info, warn, error, or off")

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
		state.dumpCommand(),
		state.loadCommand(),
		state.menuCommand(),
		state.marketplaceCommand(),
		state.protocolsCommand(),
		state.telemetryCommand(),
		state.validateCommand(),
	)
	return root
}

func (s *commandState) prepareClient(command *cobra.Command, args []string) error {
	if command == command.Root() || command.Name() == "help" {
		return nil
	}
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return nil
		}
	}
	if err := s.configureLogger(environmentLogLevel(s.runner.Getenv, s.logLevel, command.Root().PersistentFlags().Changed("log-level"))); err != nil {
		return statusError(s.runner.usageError(err.Error()))
	}
	if command.Annotations[offlineCommand] == "true" {
		return nil
	}
	settings, err := s.runner.loadConfig(s.configPath, command.Root().PersistentFlags().Changed("config"))
	if err != nil {
		return statusError(s.runner.fail(err))
	}

	loader := viper.New()
	loader.SetDefault("default_branch", "main")
	loader.SetDefault("log_level", "error")
	if err := loader.MergeConfigMap(configMap(settings)); err != nil {
		return statusError(s.runner.fail(err))
	}
	if err := loader.MergeConfigMap(environmentMap(s.runner.Getenv)); err != nil {
		return statusError(s.runner.fail(err))
	}
	flags := command.Root().PersistentFlags()
	for key, name := range map[string]string{
		"address": "address", "api_token": "token", "default_branch": "branch", "log_level": "log-level",
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
	if flags.Changed("log-level") {
		loader.Set("log_level", s.logLevel)
	}
	level, enabled, err := parseLogLevel(loader.GetString("log_level"))
	if err != nil {
		return statusError(s.runner.usageError(err.Error()))
	}
	if !s.runner.loggerInjected {
		if enabled {
			s.runner.Logger = newCLILogger(s.runner.Stderr, level)
			s.runner.logErrors = true
		} else {
			s.runner.Logger = zap.NewNop()
			s.runner.logErrors = false
		}
	}
	s.settings = sdkconfig.Config{
		Address:       loader.GetString("address"),
		APIToken:      loader.GetString("api_token"),
		DefaultBranch: loader.GetString("default_branch"),
		LogLevel:      loader.GetString("log_level"),
	}
	if s.settings.Address == "" {
		if command.Annotations[optionalClientCommand] == "true" {
			return nil
		}
		return statusError(s.runner.usageError("infrahubctl: address is required in --address, INFRAHUB_ADDRESS, or the config file"))
	}
	s.client, err = s.settings.NewClient()
	if err != nil {
		return statusError(s.runner.fail(err))
	}
	return nil
}

func (s *commandState) configureLogger(value string) error {
	if s.runner.loggerInjected {
		return nil
	}
	level, enabled, err := parseLogLevel(value)
	if err != nil {
		return err
	}
	if enabled {
		s.runner.Logger = newCLILogger(s.runner.Stderr, level)
		s.runner.logErrors = true
	} else {
		s.runner.Logger = zap.NewNop()
		s.runner.logErrors = false
	}
	return nil
}

func environmentLogLevel(getenv func(string) string, flagValue string, flagSet bool) string {
	if flagSet {
		return flagValue
	}
	if value := getenv(sdkconfig.EnvLogLevel); value != "" {
		return value
	}
	return "error"
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
	if settings.LogLevel != "" {
		result["log_level"] = settings.LogLevel
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
	if value := getenv(sdkconfig.EnvLogLevel); value != "" {
		result["log_level"] = value
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
			return s.execute(command, func() int { return run(args) })
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

func (s *commandState) execute(command *cobra.Command, run func() int) error {
	started := time.Now()
	s.runner.Logger.Info("command started", zap.String("command", command.CommandPath()))
	code := run()
	s.runner.Logger.Info("command finished",
		zap.String("command", command.CommandPath()),
		zap.Int("exit_code", code),
		zap.Duration("duration", time.Since(started)),
	)
	return statusError(code)
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

func (s *commandState) dumpCommand() *cobra.Command {
	return s.leaf("dump", func(args []string) int {
		return s.runner.runDump(s.ctx, s.client, s.settings.DefaultBranch, args)
	})
}

func (s *commandState) loadCommand() *cobra.Command {
	return s.leaf("load", func(args []string) int {
		return s.runner.runLoad(s.ctx, s.client, s.settings.DefaultBranch, args)
	})
}

func (s *commandState) menuCommand() *cobra.Command {
	command := s.group("menu", func(args []string) int {
		return s.runner.runMenu(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "load", "validate")
	command.Commands()[1].Annotations = map[string]string{offlineCommand: "true"}
	return command
}

func (s *commandState) marketplaceCommand() *cobra.Command {
	command := s.group("marketplace", func(args []string) int {
		return s.runner.runMarketplace(s.ctx, args)
	}, "list", "search", "show", "get")
	for _, child := range command.Commands() {
		child.Annotations = map[string]string{offlineCommand: "true"}
	}
	return command
}

func (s *commandState) protocolsCommand() *cobra.Command {
	command := s.leaf("protocols", func(args []string) int {
		return s.runner.runProtocols(s.ctx, s.client, s.settings.DefaultBranch, args)
	})
	command.Annotations = map[string]string{optionalClientCommand: "true"}
	return command
}

func (s *commandState) telemetryCommand() *cobra.Command {
	return s.group("telemetry", func(args []string) int {
		return s.runner.runTelemetry(s.ctx, s.client, args)
	}, "list", "export")
}

func (s *commandState) validateCommand() *cobra.Command {
	command := s.group("validate", func(args []string) int {
		return s.runner.runValidate(s.ctx, s.client, s.settings.DefaultBranch, args)
	}, "schema", "graphql-query")
	command.Commands()[1].Annotations = map[string]string{offlineCommand: "true"}
	return command
}
