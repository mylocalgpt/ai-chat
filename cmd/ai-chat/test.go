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

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	runner := testing.NewTestRunner(*verbose, *scenario)
	report := runner.Run()

	report.Print()
	os.Exit(report.ExitCode())
}
