// Package main implements a simple CLI greeting tool.
// It takes a name as a command-line argument and prints a greeting.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: greeting <name>")
		os.Exit(1)
	}

	name := strings.Join(os.Args[1:], " ")
	fmt.Println(greet(name))
}

// greet returns a greeting string for the given name.
func greet(name string) string {
	if name == "" {
		return "Hello, World!"
	}
	return fmt.Sprintf("Hello, %s!", name)
}
