package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Pull latest source and rebuild every tool via its setup.sh",
	RunE:  runUpdate,
}

func runUpdate(cmd *cobra.Command, args []string) error {
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

		if err := installTool(t); err != nil {
			fmt.Printf("  %s %s  setup.sh failed: %s\n", nameStyle.Render(t.name), crossMark, err)
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
	before, _, _ := strings.Cut(s, "\n")
	return before
}
