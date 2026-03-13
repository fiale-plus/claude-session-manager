package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/client"
	"github.com/pchaganti/claude-session-manager/tui-go/internal/tui"
)

func main() {
	socketPath := "/tmp/csm-ctl.sock"
	if envSock := os.Getenv("CSM_CTL_SOCK"); envSock != "" {
		socketPath = envSock
	}

	c := client.New(socketPath)
	model := tui.NewModel(c)

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
