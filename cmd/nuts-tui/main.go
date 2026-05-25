package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sig-cloudnative/nuts/pkg/tui"
)

var (
	serverURL = flag.String("server", "tcp://localhost:8080", "Nuts server address (tcp://host:port or unix:///path/to/socket)")
	authToken = flag.String("token", "", "API auth token from server log")
	version   = "dev"
)

func main() {
	flag.Parse()

	token := *authToken
	if token == "" {
		token = os.Getenv("NUTS_AUTH_TOKEN")
	}

	fmt.Println("🥜 Nuts TUI - Terminal User Interface")
	fmt.Printf("Server: %s\n\n", *serverURL)

	app := tui.NewAppWithToken(*serverURL, token)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
