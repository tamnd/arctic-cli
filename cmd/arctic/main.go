// Command arctic acquires, processes, and queries the public Reddit archive.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/fang"
	"github.com/tamnd/arctic-cli/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cli.NewRootCmd()
	err := fang.Execute(ctx, root,
		fang.WithVersion(cli.Version),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
	if err == nil {
		return
	}

	var ee *cli.ExitError
	if errors.As(err, &ee) {
		if ee.Err != nil {
			fmt.Fprintln(os.Stderr, "arctic:", ee.Err)
		}
		os.Exit(ee.Code)
	}
	fmt.Fprintln(os.Stderr, "arctic:", err)
	os.Exit(1)
}
