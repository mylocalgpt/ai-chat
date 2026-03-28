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
	case "usage":
		runAuditUsage(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown audit subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runAuditCheck(args []string) {
	fs := flag.NewFlagSet("audit check", flag.ExitOnError)
	days := fs.Int("days", 1, "number of days to check")
	logDir := fs.String("log-dir", "", "log directory (default ~/.config/ai-chat/logs/)")
	_ = fs.Parse(args)

	dir := *logDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		dir = filepath.Join(home, ".config", "ai-chat", "logs")
	}

	result, err := audit.RunAuditCheck(dir, *days)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(result.String())
}

func runAuditUsage(args []string) {
	fs := flag.NewFlagSet("audit usage", flag.ExitOnError)
	workspace := fs.String("workspace", "", "filter by workspace name (default: all)")
	days := fs.Int("days", 7, "number of days to aggregate")
	logDir := fs.String("log-dir", "", "log directory (default ~/.config/ai-chat/logs/)")
	_ = fs.Parse(args)

	dir := *logDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		dir = filepath.Join(home, ".config", "ai-chat", "logs")
	}

	if *workspace != "" {
		u, err := audit.UsageSummary(dir, *workspace, *days)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(u.String())
	} else {
		summaries, err := audit.AllUsageSummaries(dir, *days)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(summaries) == 0 {
			fmt.Println("No usage data found.")
			return
		}
		for _, u := range summaries {
			fmt.Print(u.String())
			fmt.Println()
		}
	}
}
