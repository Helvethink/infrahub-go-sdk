// Command infrahubctl provides a command-line client for Infrahub.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Helvethink/infrahub-go-sdk/internal/cli"
)

var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := cli.Runner{Build: cli.BuildInfo{Version: version, Commit: commit, Date: date}}
	os.Exit(runner.Run(ctx, os.Args[1:]))
}
