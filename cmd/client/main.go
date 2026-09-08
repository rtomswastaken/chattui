package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rtoms/chattui/internal/client"
	"github.com/rtoms/chattui/internal/tui"
)

func main() {
	serverAddr := flag.String("server", "127.0.0.1:8443", "chatTUI server address (IP:port or Tailscale hostname:port)")
	flag.Parse()

	var p *tea.Program

	c := client.NewClient(*serverAddr, func(state client.ConnState) {
		if p != nil {
			p.Send(tui.ConnStateMsg(state))
		}
	})

	// Initial connection attempt
	_ = c.Connect()

	// Start auto-reconnection loop in background
	c.StartAutoReconnect()

	app := tui.NewApp(c)
	p = tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running chatTUI: %v\n", err)
		c.Close()
		os.Exit(1)
	}

	c.Close()
}
