package main

import (
	"fmt"
	"os"

	"agentplane/internal/version"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(version.String())
		return
	}

	fmt.Fprintln(os.Stderr, "agentplane: execution core is not implemented yet")
	fmt.Fprintln(os.Stderr, "usage: agentplane version")
	os.Exit(2)
}
