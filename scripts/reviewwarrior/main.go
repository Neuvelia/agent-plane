// Command reviewwarrior supports the ReviewWarrior pull request review workflow
// (.github/workflows/review-warrior.yml). It composes the reviewer prompt,
// selects author responses, checks the shape of a review and renders the pull
// request comment. It never calls GitHub or the model: the workflow does, and
// passes everything in as files.
//
// The workflow builds this command from the pull request's base commit, so a
// pull request cannot change the code that composes its own review.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var errUsage = errors.New("usage: reviewwarrior compose|responses|check|render|render-failure|git-guard [flags]")

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "reviewwarrior:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	commands := map[string]func([]string, io.Writer) error{
		"compose":        runCompose,
		"responses":      runResponses,
		"check":          runCheck,
		"render":         runRender,
		"render-failure": runRenderFailure,
		"git-guard":      runGitGuard,
	}
	command, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q: %w", args[0], errUsage)
	}
	return command(args[1:], out)
}
