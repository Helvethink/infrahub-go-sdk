package cli

import (
	"context"
	"strings"

	flag "github.com/spf13/pflag"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
)

func (r Runner) runTask(ctx context.Context, client *infrahub.Client, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] task <list>")
	}
	switch args[0] {
	case "list":
		command := flag.NewFlagSet("task list", flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		states := multiFlag{}
		command.Var(&states, "state", "filter by task state")
		limit := command.Int("limit", 0, "maximum number of tasks")
		offset := command.Int("offset", 0, "pagination offset")
		includeRelated := command.Bool("include-related-nodes", false, "include related nodes")
		includeLogs := command.Bool("include-logs", false, "include logs")
		_ = command.Bool("json", true, "accepted for compatibility; JSON is always written")
		_ = command.Bool("debug", false, "accepted for compatibility")
		if err := parseInterspersed(command, args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() != 0 {
			return r.usageError("usage: infrahubctl task list [flags]")
		}
		taskStates := make([]infrahub.TaskState, 0, len(states))
		for _, state := range states {
			taskStates = append(taskStates, infrahub.TaskState(strings.ToUpper(state)))
		}
		page, err := client.Tasks.List(ctx, infrahub.TaskListOptions{
			Filter: infrahub.TaskFilter{States: taskStates}, Offset: *offset, Limit: *limit,
			IncludeLogs: *includeLogs, IncludeRelatedNodes: *includeRelated,
		})
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(page)
	default:
		return r.usageError("infrahubctl: unknown task command " + args[0])
	}
}
