package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Pull latest source and rebuild every tool via its setup.sh",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("✓")
	crossMark := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("✗")
	nameStyle := lipgloss.NewStyle().Width(14)

	root := expandHome("~/Developing/Projects/missionctl")

	fmt.Println()
	updated, failed := 0, []string{}

	for _, t := range tools {
		toolPath := filepath.Join(root, t.project)
		if _, err := os.Stat(filepath.Join(toolPath, ".git")); err != nil {
			fmt.Printf("  %s %s  no local checkout, skipped\n", nameStyle.Render(t.name), crossMark)
			continue
		}

		pull := exec.Command("git", "-C", toolPath, "pull", "--ff-only")
		if out, err := pull.CombinedOutput(); err != nil {
			fmt.Printf("  %s %s  git pull failed: %s\n", nameStyle.Render(t.name), crossMark, firstLine(string(out)))
			failed = append(failed, t.name)
			continue
		}

		setup := exec.Command("bash", filepath.Join(toolPath, "setup.sh"))
		setup.Dir = toolPath
		if out, err := setup.CombinedOutput(); err != nil {
			fmt.Printf("  %s %s  setup.sh failed: %s\n", nameStyle.Render(t.name), crossMark, firstLine(string(out)))
			failed = append(failed, t.name)
			continue
		}

		fmt.Printf("  %s %s  rebuilt & reinstalled\n", nameStyle.Render(t.name), checkMark)
		updated++
	}

	fmt.Println()
	fmt.Printf("  %d/%d tools updated\n", updated, len(tools))
	fmt.Println()

	if len(failed) > 0 {
		return fmt.Errorf("%d tool(s) failed to update: %v", len(failed), failed)
	}
	return nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
