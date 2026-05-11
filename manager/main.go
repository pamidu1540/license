package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := loadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	if cfg.AdminKey == "" {
		fmt.Fprintln(os.Stderr,
			"Admin key not configured.\n"+
				"Set LM_ADMIN_KEY env var or add admin_key to\n"+
				"~/.config/logistics-license-manager/config.toml")
		os.Exit(1)
	}

	p := tea.NewProgram(
		newModel(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
