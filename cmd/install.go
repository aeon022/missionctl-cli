package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var installAll bool

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Build and install every tool in one go via its setup.sh",
	RunE:  runInstall,
}

func runInstall(cmd *cobra.Command, args []string) error {
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("✓")
	crossMark := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("✗")
	dashMark := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("–")
	nameStyle := lipgloss.NewStyle().Width(14)

	root := expandHome("~/Developing/Projects/missionctl")

	fmt.Println()
	installed, failed := 0, []string{}

	for _, t := range tools {
		if !installAll {
			if _, err := exec.LookPath(t.name); err == nil {
				fmt.Printf("  %s %s  already installed\n", nameStyle.Render(t.name), dashMark)
				continue
			}
		}

		toolPath := filepath.Join(root, t.project)
		setup := exec.Command("bash", filepath.Join(toolPath, "setup.sh"))
		setup.Dir = toolPath
		if out, err := setup.CombinedOutput(); err != nil {
			fmt.Printf("  %s %s  setup.sh failed: %s\n", nameStyle.Render(t.name), crossMark, firstLine(string(out)))
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
