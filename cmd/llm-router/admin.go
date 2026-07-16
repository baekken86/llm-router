package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/chris/llm-router/internal/config"
	"github.com/chris/llm-router/internal/proxy"
	"github.com/chris/llm-router/internal/tui"
)

func runAdmin(args []string) {
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	connect := fs.String("connect", "http://localhost:8080", "Proxy URL to connect to")
	apiKey := fs.String("key", "", "Proxy API key")
	fs.Parse(args)

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "Error: --key is required")
		fmt.Fprintln(os.Stderr, "Usage: llm-router admin --connect http://proxy:8080 --key lmr_...")
		os.Exit(1)
	}

	fmt.Printf("Connecting to %s...\n", *connect)

	client := tui.NewAPIClient(*connect, *apiKey)

	stats, err := client.GetStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot connect to proxy: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected. Proxy has %d total requests.\n", stats.TotalRequests)
	fmt.Println("Starting admin dashboard... (press q to quit)")

	logChan := make(chan proxy.RequestLog, 100)

	go func() {
		err := client.StreamLogs(logChan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Log stream error: %v\n", err)
		}
	}()

	cfg := config.New()
	tui.RunRemote(client, logChan, cfg)
}
