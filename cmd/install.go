package cmd

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var installAll bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Build and install every tool in one go via its setup.sh",
	RunE:  runInstall,
}

func runInstall(cmd *cobra.Command, args []string) error {
	fmt.Println()
	installed, failed := 0, []string{}

	for _, t := range tools {
		if !installAll {
			if _, err := exec.LookPath(t.name); err == nil {
				fmt.Printf("  %s %s  already installed\n", nameStyle.Render(t.name), dashMark)
				continue
			}
		}

		if err := installTool(t); err != nil {
			fmt.Printf("  %s %s  setup.sh failed: %s\n", nameStyle.Render(t.name), crossMark, err)
			failed = append(failed, t.name)
			continue
		}

		fmt.Printf("  %s %s  installed\n", nameStyle.Render(t.name), checkMark)
		installed++
	}

	fmt.Println()
	fmt.Printf("  %d tool(s) newly installed\n", installed)
	fmt.Println()

	if len(failed) > 0 {
		return fmt.Errorf("%d tool(s) failed to install: %v", len(failed), failed)
	}
	return nil
}

func init() {
	installCmd.Flags().BoolVar(&installAll, "all", false, "Reinstall every tool, even ones already on PATH")
}
