// Command tmux-agents watches AI coding agents running in tmux panes.
package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run dispatches subcommands. Later tasks replace the stub cases.
func run(args []string, stdout io.Writer) int {
	cmd := "sidebar"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version":
		fmt.Fprintf(stdout, "tmux-agents %s\n", version)
		return 0
	case "sidebar", "summary", "jump", "watch":
		fmt.Fprintf(stdout, "%s: not implemented yet\n", cmd)
		return 1
	default:
		fmt.Fprintf(stdout, "usage: tmux-agents [sidebar|summary|jump|watch|version]\n")
		return 2
	}
}
