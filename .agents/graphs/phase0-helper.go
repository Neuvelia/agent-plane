// This small portable process lets the example graph show bound data flow.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: phase0-helper left|right|join")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "left", "right":
		message, present := os.LookupEnv("MESSAGE")
		if present {
			fmt.Printf("%s:%s\n", os.Args[1], message)
		} else {
			fmt.Println(os.Args[1])
		}
	case "join":
		left, hasLeft := os.LookupEnv("LEFT_TEXT")
		right, hasRight := os.LookupEnv("RIGHT_TEXT")
		if !hasLeft || !hasRight {
			fmt.Fprintln(os.Stderr, "join requires LEFT_TEXT and RIGHT_TEXT")
			os.Exit(2)
		}
		fmt.Printf("%s|%s\n", strings.TrimSuffix(left, "\n"), strings.TrimSuffix(right, "\n"))
	default:
		fmt.Fprintf(os.Stderr, "unknown phase0-helper mode %q\n", os.Args[1])
		os.Exit(2)
	}
}
