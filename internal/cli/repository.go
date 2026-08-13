package cli

import (
	"context"

	flag "github.com/spf13/pflag"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
)

func (r Runner) runRepository(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] repository <list>")
	}
	switch args[0] {
	case "list":
		command := flag.NewFlagSet("repository list", flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		commandBranch := command.String("branch", branch, "branch on which to list repositories")
		_ = command.Bool("debug", false, "accepted for compatibility")
		if err := parseInterspersed(command, args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() != 0 {
			return r.usageError("usage: infrahubctl repository list [flags]")
		}
		repositories, err := client.Repositories.List(ctx, infrahub.RepositoryListOptions{Branches: []string{*commandBranch}})
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(repositories)
	default:
		return r.usageError("infrahubctl: repository command " + args[0] + " is not implemented in the Go CLI")
	}
}
