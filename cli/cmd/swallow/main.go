// Command swallow is the operator command-line client for the Swallow platform.
//
// It is the entry point of the `cli` component: a thin main that wires OS signal
// handling to context cancellation and delegates the entire command tree to the
// command package. Keeping main minimal means the testable behavior lives in the
// internal packages rather than in package main.
//
// Exit codes: 0 on success, 1 on any error (including an api-server error
// envelope). The error is printed to stderr; structured results go to stdout so
// the two can be redirected independently.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/maple52046/swallow/cli/internal/command"
)

func main() {
	// SIGINT/SIGTERM cancel the command context so long-running calls — notably
	// `servers watch` — stop cleanly instead of leaving a dangling connection.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := command.NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
