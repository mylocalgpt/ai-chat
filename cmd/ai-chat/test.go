package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mylocalgpt/ai-chat/pkg/testing"
)

func runTest(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "print detailed output for each assertion")
	scenario := fs.String("scenario", "", "run only the named scenario")
	configPath := fs.String("config", "", "path to config file for explicit acceptance runs")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *scenario == "telegram-acceptance" {
		if err := runTelegramAcceptance(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "telegram acceptance failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("telegram acceptance passed")
		return
	}

	runner := testing.NewTestRunner(*verbose, *scenario)
	report := runner.Run()

	report.Print()
	os.Exit(report.ExitCode())
}
