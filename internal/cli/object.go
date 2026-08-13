package cli

import (
	"context"
	"fmt"
	"strings"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
	"github.com/Helvethink/infrahub-go-sdk/pkg/node"
	flag "github.com/spf13/pflag"
)

func (r Runner) runObject(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] object <get|create|update|delete>")
	}
	switch args[0] {
	case "get":
		return r.runObjectGet(ctx, client, branch, args[1:])
	case "create":
		return r.runObjectCreate(ctx, client, branch, args[1:])
	case "update":
		return r.runObjectUpdate(ctx, client, branch, args[1:])
	case "delete":
		return r.runObjectDelete(ctx, client, branch, args[1:])
	case "load":
		return r.runObjectLoad(ctx, client, branch, args[1:])
	case "validate":
		return r.runObjectValidate(args[1:])
	default:
		return r.usageError("infrahubctl: unknown object command " + args[0])
	}
}

func (r Runner) runObjectGet(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("object get", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	filters := multiFlag{}
	command.Var(&filters, "filter", "filter in key=value format")
	commandBranch := command.String("branch", branch, "target branch")
	output := command.String("output", "json", "output format")
	limit := command.Int("limit", 0, "maximum results")
	offset := command.Int("offset", 0, "pagination offset")
	_ = command.Bool("all-columns", false, "accepted for compatibility")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if *output != "json" {
		return r.usageError("only -output json is supported by the Go CLI")
	}
	if command.NArg() < 1 || command.NArg() > 2 {
		return r.usageError("usage: infrahubctl object get [flags] <kind> [identifier]")
	}
	kind := command.Arg(0)
	if command.NArg() == 2 {
		node, err := client.Nodes.GetByHFID(ctx, kind, strings.Split(command.Arg(1), "/"), *commandBranch)
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(node)
	}
	parsedFilters, err := parseAssignments(filters, false)
	if err != nil {
		return r.usageError(err.Error())
	}
	queryFilters := make([]node.Filter, 0, len(parsedFilters))
	for name, value := range parsedFilters {
		queryFilters = append(queryFilters, node.Filter{Name: name, Value: value})
	}
	page, err := client.Nodes.Query(ctx, kind, node.QueryOptions{
		Branch: *commandBranch, Offset: *offset, Limit: *limit, Filters: queryFilters,
	})
	if err != nil {
		return r.fail(err)
	}
	if len(page.Nodes) == 0 {
		return 80
	}
	return r.writeJSON(page)
}

func (r Runner) runObjectCreate(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("object create", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	values := multiFlag{}
	command.Var(&values, "set", "field value in key=value format")
	file := command.String("file", "", "JSON or YAML file with object data")
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 1 {
		return r.usageError("usage: infrahubctl object create [flags] <kind>")
	}
	data, err := objectMutationData(values, *file)
	if err != nil {
		return r.usageError(err.Error())
	}
	node, err := client.Nodes.Create(ctx, command.Arg(0), data, *commandBranch)
	if err != nil {
		return r.fail(err)
	}
	return r.writeJSON(node)
}

func (r Runner) runObjectUpdate(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("object update", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	values := multiFlag{}
	command.Var(&values, "set", "field value in key=value format")
	file := command.String("file", "", "JSON or YAML file with update data")
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 2 {
		return r.usageError("usage: infrahubctl object update [flags] <kind> <identifier>")
	}
	data, err := objectMutationData(values, *file)
	if err != nil {
		return r.usageError(err.Error())
	}
	data["hfid"] = strings.Split(command.Arg(1), "/")
	node, err := client.Nodes.Update(ctx, command.Arg(0), data, *commandBranch)
	if err != nil {
		return r.fail(err)
	}
	return r.writeJSON(node)
}

func (r Runner) runObjectDelete(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("object delete", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	yes := command.Bool("yes", false, "skip confirmation prompt")
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 2 {
		return r.usageError("usage: infrahubctl object delete [flags] <kind> <identifier>")
	}
	if !*yes {
		return r.usageError("object delete requires --yes in non-interactive Go CLI")
	}
	err := client.Nodes.Delete(ctx, command.Arg(0), map[string]any{"hfid": strings.Split(command.Arg(1), "/")}, *commandBranch)
	if err != nil {
		return r.fail(err)
	}
	if _, err := fmt.Fprintln(r.Stdout, "delete: ok"); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}

func objectMutationData(values []string, file string) (map[string]any, error) {
	if len(values) > 0 && file != "" {
		return nil, fmt.Errorf("--set and --file are mutually exclusive")
	}
	if file != "" {
		return readDataFile(file)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one --set value or --file is required")
	}
	return parseAssignments(values, true)
}

func (r Runner) runObjectLoad(ctx context.Context, client *infrahub.Client, branch string, args []string) int {
	command := flag.NewFlagSet("object load", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	commandBranch := command.String("branch", branch, "target branch")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() == 0 {
		return r.usageError("usage: infrahubctl object load [flags] <object-file-or-directory>...")
	}
	documents, err := readObjectDocuments(command.Args())
	if err != nil {
		return r.fail(err)
	}
	results := make([]*infrahub.Node, 0, len(documents))
	for _, document := range documents {
		for _, item := range document.Data {
			node, err := client.Nodes.Upsert(ctx, document.Kind, normalizeObjectData(item), *commandBranch)
			if err != nil {
				return r.fail(err)
			}
			results = append(results, node)
		}
	}
	return r.writeJSON(map[string]any{"count": len(results), "objects": results})
}

func (r Runner) runObjectValidate(args []string) int {
	command := flag.NewFlagSet("object validate", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() == 0 {
		return r.usageError("usage: infrahubctl object validate <object-file-or-directory>...")
	}
	documents, err := readObjectDocuments(command.Args())
	if err != nil {
		return r.fail(err)
	}
	return r.writeJSON(map[string]any{"valid": true, "documents": len(documents)})
}
