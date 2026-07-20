package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/aeon022/missionctl-cli/cmd.Version=v1.2.3".
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "missionctl",
	Short:   "Umbrella CLI for the missionctl suite",
	Version: Version,
	Long: `missionctl — control plane for your personal terminal stack.

Run with no arguments for the dashboard TUI (today's briefing across every
tool, with a keypress to jump straight into any of them).

Commands:
  doctor   Check which tools are installed and env vars set
  status   Daily briefing from all tool databases
  init     Interactive setup wizard
  install  Build and install every tool in one go
  update   Pull latest source and rebuild every tool`,
	RunE: runDashboard,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(updateCmd)
}
