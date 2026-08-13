package cli

import (
	"context"
	"time"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	flag "github.com/spf13/pflag"
)

func (r Runner) runDiff(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] diff <tree|summary>")
	}
	switch args[0] {
	case "tree", "summary":
		command := flag.NewFlagSet("diff "+args[0], flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		name := command.String("name", "", "diff name")
		from := command.String("from", "", "RFC3339 lower time bound")
		to := command.String("to", "", "RFC3339 upper time bound")
		if err := parseInterspersed(command, args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() > 1 {
			return r.usageError("usage: infrahubctl diff " + args[0] + " [flags] [branch]")
		}
		diffBranch := branch
		if command.NArg() == 1 {
			diffBranch = command.Arg(0)
		}
		options := infrahub.DiffOptions{Branch: diffBranch, Name: *name}
		var err error
		options.FromTime, err = parseOptionalTime(*from)
		if err != nil {
			return r.usageError(err.Error())
		}
		options.ToTime, err = parseOptionalTime(*to)
		if err != nil {
			return r.usageError(err.Error())
		}
		if args[0] == "summary" {
			nodes, err := client.Diffs.Summary(ctx, options)
			if err != nil {
				return r.fail(err)
			}
			return r.writeJSON(nodes)
		}
		tree, err := client.Diffs.Tree(ctx, options)
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(tree)
	default:
		return r.usageError("infrahubctl: unknown diff command " + args[0])
	}
}

func parseOptionalTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}
