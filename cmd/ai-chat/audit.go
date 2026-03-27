package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mylocalgpt/ai-chat/pkg/audit"
)

func runAudit(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: ai-chat audit <check|usage>\n")
		os.Exit(1)
	}
	switch args[0] {
	case "check":
		runAuditCheck(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown audit subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runAuditCheck(args []string) {
	fs := flag.NewFlagSet("audit check", flag.ExitOnError)
	days := fs.Int("days", 1, "number of days to check")
	logDir := fs.String("log-dir", "", "log directory (default ~/.ai-chat/logs/)")
	fs.Parse(args)

	dir := *logDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		dir = filepath.Join(home, ".ai-chat", "logs")
	}

	result, err := audit.RunAuditCheck(dir, *days)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(result.String())
}
