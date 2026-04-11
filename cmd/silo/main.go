// Command silo is the unified Silo binary. It dispatches to the fileserver
// daemon (`silo serve`), the Bubble Tea TUI (`silo tui`), or one of the
// non-interactive CLI subcommands (`silo ls`, `silo get`, …).
package main

import (
	"fmt"
	"os"

	"github.com/dkam/silo/fileserver" // package silod
	"github.com/dkam/silo/internal/cli"
	"github.com/dkam/silo/internal/tui"
)

const defaultServerURL = "http://localhost:8082"

// Version is stamped at build time via -ldflags "-X main.Version=...".
// The default is the current source-tree version; CI overrides it with
// `git describe --tags --always --dirty` so tagged builds report the tag.
var Version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "serve":
		if err := silod.Run(rest); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "tui":
		url := serverURL()
		if len(rest) > 0 && rest[0] != "" {
			url = rest[0]
		}
		if err := tui.Run(url, email(), password()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	case "version", "-v", "--version":
		fmt.Println(Version)
	default:
		if err := cli.Run(serverURL(), email(), password(), args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// serverURL/email/password read the SILO_* env vars, falling back to the
// legacy SEAFILE_* names so existing scripts keep working.

func serverURL() string {
	if v := os.Getenv("SILO_URL"); v != "" {
		return v
	}
	if v := os.Getenv("SEAFILE_URL"); v != "" {
		return v
	}
	// Fall back to the same host/port the server daemon uses.
	host := os.Getenv("SEAFILE_FILESERVER_HOST")
	port := os.Getenv("SEAFILE_FILESERVER_PORT")
	if host != "" || port != "" {
		if host == "" {
			host = "127.0.0.1"
		}
		if port == "" {
			port = "8082"
		}
		return fmt.Sprintf("http://%s:%s", host, port)
	}
	return defaultServerURL
}

func email() string {
	if v := os.Getenv("SILO_EMAIL"); v != "" {
		return v
	}
	if v := os.Getenv("SEAFILE_EMAIL"); v != "" {
		return v
	}
	return os.Getenv("SEAFILE_ADMIN_EMAIL")
}

func password() string {
	if v := os.Getenv("SILO_PASSWORD"); v != "" {
		return v
	}
	if v := os.Getenv("SEAFILE_PASSWORD"); v != "" {
		return v
	}
	return os.Getenv("SEAFILE_ADMIN_PASSWORD")
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `silo — Seafile-compatible server and client in one binary

Usage:
  silo serve [flags]              Run the file server daemon
  silo tui [url]                  Launch the interactive terminal UI
  silo repos [--json]             List libraries
  silo repo create <name>         Create a library (prints ID)
  silo repo rm <repo-id>          Delete a library
  silo ls <repo-id> [path] [--json]
  silo get <repo-id> <remote> [local]
  silo put <repo-id> <local> [remote-dir]
  silo mkdir <repo-id> <path>
  silo rm <repo-id> <path>
  silo mv <repo-id> <src> <dst>
  silo rename <repo-id> <path> <new-name>
  silo version                    Print the build version

Environment:
  SILO_URL         Server base URL (default http://localhost:8082)
  SILO_EMAIL       Account email for TUI/CLI
  SILO_PASSWORD    Account password for TUI/CLI
  SEAFILE_*        Honored as a fallback for the SILO_* variables

Run "silo serve -h" for server-side flags.
`)
}
