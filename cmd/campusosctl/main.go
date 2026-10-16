package main

import (
	"fmt"
	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "plugin":
		return runPlugin(args[1:], stdout, stderr)
	case "resource":
		return runResource(args[1:], stdout, stderr)
	case "identity":
		return runIdentity(args[1:], stdout, stderr, os.Stdin)
	case "storage":
		return runStorage(args[1:], stdout, stderr)
	case "schedule":
		return runSchedule(args[1:], stdout, stderr)
	case "secret":
		return runSecret(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--short") {
		fmt.Fprintln(stderr, "usage: campusosctl version [--short]")
		return 2
	}
	if len(args) == 1 {
		fmt.Fprintln(stdout, platformversion.Number)
		return 0
	}
	fmt.Fprintln(stdout, platformversion.Display)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: campusosctl <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  version   print the CampusOS application version")
	fmt.Fprintln(w, "  plugin    plugin scaffolding and inspection")
	fmt.Fprintln(w, "  resource  resource package adoption and inspection")
	fmt.Fprintln(w, "  identity  local system-account recovery commands")
	fmt.Fprintln(w, "  storage   local user-storage reconciliation commands")
	fmt.Fprintln(w, "  schedule  historical schedule adoption commands")
	fmt.Fprintln(w, "  secret    local plugin Secret key rotation commands")
}
