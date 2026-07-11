package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:     "missionctl",
	Short:   "Umbrella CLI for the missionctl suite",
	Version: version,
	Long: `missionctl — control plane for your personal terminal stack.

Commands:
  doctor   Check which tools are installed and env vars set
  status   Daily briefing from all tool databases
  init     Interactive setup wizard`,
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
}
