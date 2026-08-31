package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	flag "github.com/spf13/pflag"

	infrahub "github.com/Helvethink/infrahub-go-sdk"
)

// runObjectStore runs the object store.
func (r Runner) runObjectStore(ctx context.Context, client *infrahub.Client, args []string) int {
	if len(args) == 0 {
		return r.usageError("usage: infrahubctl [global flags] objectstore <get|upload|file>")
	}
	switch args[0] {
	case "get":
		if len(args) != 2 {
			return r.usageError("usage: infrahubctl objectstore get <identifier>")
		}
		content, err := client.ObjectStore.Get(ctx, args[1])
		if err != nil {
			return r.fail(err)
		}
		return r.writeText(content)
	case "upload":
		command := flag.NewFlagSet("objectstore upload", flag.ContinueOnError)
		command.SetOutput(r.Stderr)
		content := command.String("content", "", "content to upload; read stdin when empty")
		if err := parseInterspersed(command, args[1:]); err != nil {
			return flagExitCode(err)
		}
		if command.NArg() != 0 {
			return r.usageError("usage: infrahubctl objectstore upload [flags]")
		}
		if *content == "" {
			data, err := io.ReadAll(r.Stdin)
			if err != nil {
				return r.fail(fmt.Errorf("read upload content: %w", err))
			}
			*content = string(data)
		}
		result, err := client.ObjectStore.Upload(ctx, *content)
		if err != nil {
			return r.fail(err)
		}
		return r.writeJSON(result)
	case "file":
		return r.runObjectStoreFile(ctx, client, args[1:])
	default:
		return r.usageError("infrahubctl: unknown objectstore command " + args[0])
	}
}

// runObjectStoreFile runs the object store file.
func (r Runner) runObjectStoreFile(ctx context.Context, client *infrahub.Client, args []string) int {
	command := flag.NewFlagSet("objectstore file", flag.ContinueOnError)
	command.SetOutput(r.Stderr)
	storageID := command.String("storage-id", "", "storage identifier")
	nodeID := command.String("id", "", "file node ID")
	kind := command.String("kind", "", "file kind for HFID lookup")
	hfid := command.String("hfid", "", "slash-separated HFID components")
	if err := parseInterspersed(command, args); err != nil {
		return flagExitCode(err)
	}
	if command.NArg() != 0 {
		return r.usageError("usage: infrahubctl objectstore file [flags]")
	}
	selected := 0
	for _, value := range []string{*storageID, *nodeID, *hfid} {
		if value != "" {
			selected++
		}
	}
	if selected != 1 || *hfid != "" && *kind == "" {
		return r.usageError("set exactly one of --storage-id, --id, or --hfid with --kind")
	}
	var (
		content string
		err     error
	)
	switch {
	case *storageID != "":
		content, err = client.ObjectStore.GetFileByStorageID(ctx, *storageID)
	case *nodeID != "":
		content, err = client.ObjectStore.GetFileByID(ctx, *nodeID)
	default:
		content, err = client.ObjectStore.GetFileByHFID(ctx, *kind, strings.Split(*hfid, "/"))
	}
	if err != nil {
		return r.fail(err)
	}
	return r.writeText(content)
}

// writeText writes the text.
func (r Runner) writeText(value string) int {
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	if _, err := io.WriteString(r.Stdout, value); err != nil {
		return r.fail(fmt.Errorf("write output: %w", err))
	}
	return 0
}
