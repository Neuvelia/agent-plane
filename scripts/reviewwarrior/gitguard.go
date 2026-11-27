package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// The reviewer may run `git diff`, `git show`, `git log` and `git ls-files`.
// `git diff` given a path outside the repository silently switches to
// `--no-index` and prints that file, so a permission pattern on the command
// text cannot keep it inside the checkout. The workflow puts a `git` shim
// first on the reviewer's PATH that runs `reviewwarrior git-guard`, which
// checks the arguments after the shell has expanded them and only then runs
// the real Git.

var errGitRefused = errors.New("git command refused by the review guard")

// allowedGitCommands are the read-only subcommands the reviewer may run.
var allowedGitCommands = map[string]bool{"diff": true, "show": true, "log": true, "ls-files": true}

// refusedGitOptions read or write files outside the object database, or
// run a helper program.
var refusedGitOptions = []string{"--no-index", "--output", "--ext-diff", "--textconv", "--open-files-in-pager", "-O"}

// escapingPath matches an argument, or an option's value, that names a path
// outside the checkout: absolute, home-relative or climbing with `..`. A
// short option takes its value from the rest of a bundle (`-pO/etc/x`,
// `-cX/etc/x`), so any run of letters after one dash counts as the option.
// Revision ranges (`a..b`, `a...b`) do not match: their dots are not a
// whole path segment.
var escapingPath = regexp.MustCompile(`^(-[A-Za-z]+|[^=]*=)?(/|~)|(^|[/=])\.\.(/|$)|^-[A-Za-z]+\.\.(/|$)`)

// shortOptionBundle matches one-dash options, alone or bundled (`-p`, `-pO`).
var shortOptionBundle = regexp.MustCompile(`^-[A-Za-z]`)

// checkGitArgs accepts args (without the leading "git") only when they run an
// allowed subcommand on paths inside the checkout.
func checkGitArgs(args []string) error {
	if len(args) == 0 || !allowedGitCommands[args[0]] {
		return fmt.Errorf("%w: only git diff, show, log and ls-files are allowed", errGitRefused)
	}
	for _, arg := range args[1:] {
		if err := checkGitArg(arg); err != nil {
			return err
		}
	}
	return nil
}

func checkGitArg(arg string) error {
	for _, option := range refusedGitOptions {
		if arg == option || strings.HasPrefix(arg, option+"=") {
			return fmt.Errorf("%w: option %q", errGitRefused, truncate(arg, 60))
		}
	}
	// Git reads -O from anywhere in a bundle, and which letters of a bundle
	// are options and which a value depends on each option, so any `O` in a
	// one-dash argument is refused. A pickaxe string with an `O` can be
	// passed as a separate argument (`-S Open`).
	if shortOptionBundle.MatchString(arg) && strings.Contains(arg, "O") {
		return fmt.Errorf("%w: option -O in %q (pass a -S/-G value with an O as a separate argument)", errGitRefused, truncate(arg, 60))
	}
	if escapingPath.MatchString(arg) {
		return fmt.Errorf("%w: %q leaves the checkout; use a path relative to the repository root", errGitRefused, truncate(arg, 60))
	}
	return nil
}

// gitStreams are the standard streams the guarded Git runs with.
type gitStreams struct {
	stdin          io.Reader
	stdout, stderr io.Writer
}

func runGitGuard(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("git-guard", flag.ContinueOnError)
	gitPath := flags.String("git", "", "absolute path of the real git binary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !strings.HasPrefix(*gitPath, "/") {
		return fmt.Errorf("%w: -git must be an absolute path", errMissingFlag)
	}
	return guardedGit(*gitPath, flags.Args(), gitStreams{stdin: os.Stdin, stdout: out, stderr: os.Stderr})
}

func guardedGit(gitPath string, args []string, streams gitStreams) error {
	if err := checkGitArgs(args); err != nil {
		return err
	}
	command := exec.Command(gitPath, args...)
	command.Stdin, command.Stdout, command.Stderr = streams.stdin, streams.stdout, streams.stderr
	return command.Run()
}
