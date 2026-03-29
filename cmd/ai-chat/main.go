// Build: go build -ldflags "-X main.version=$(git describe --tags --always)" -o ai-chat ./cmd/ai-chat
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "start":
		runStart(os.Args[2:])
	case "stdio":
		runStdio()
	case "audit":
		runAudit(os.Args[2:])
	case "test":
		runTest(os.Args[2:])
	case "version":
		fmt.Println("ai-chat", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: ai-chat <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  start    Start the bot\n")
	fmt.Fprintf(os.Stderr, "  stdio    Run as MCP server (stdin/stdout)\n")
	fmt.Fprintf(os.Stderr, "  audit    Run audit analysis\n")
	fmt.Fprintf(os.Stderr, "  test     Run E2E tests\n")
	fmt.Fprintf(os.Stderr, "  version  Print version\n")
}
