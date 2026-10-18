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
	Short: "Update every tool: git pull + rebuild for source checkouts, verified release binaries otherwise",
	Long: `Tools with a source checkout under ~/Developing/Projects/missionctl are updated with git pull + setup.sh.
Tools without one are updated from their latest GitHub release: the archive for this platform is downloaded,
its SHA-256 is checked against the release's checksums.txt, and the binary replaces the installed one.
Builds from source ("dev") are never overwritten by a release.

  missionctl update             update everything
  missionctl update --check     only report what would be updated
  missionctl update --binaries  use releases even where a source checkout exists`,
	RunE: runUpdate,
}

var updateCheckOnly, updateBinaries bool

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only report available updates, change nothing")
	updateCmd.Flags().BoolVar(&updateBinaries, "binaries", false, "update from GitHub releases even where a source checkout exists")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	root := expandHome("~/Developing/Projects/missionctl")

	fmt.Fprintln(cliOut)
	updated, failed := 0, []string{}

	for _, t := range tools {
		toolPath := filepath.Join(root, t.project)
		if _, err := os.Stat(filepath.Join(toolPath, ".git")); err != nil || updateBinaries {
			msg, err := updateFromRelease(cmd.Context(), newReleaseClient(), t.name, updateCheckOnly)
			switch {
			case err != nil:
				fmt.Fprintf(cliOut, "  %s %s  %s\n", nameStyle.Render(t.name), crossMark, err)
				if !strings.Contains(err.Error(), "no release published yet") {
					failed = append(failed, t.name)
				}
			default:
				fmt.Fprintf(cliOut, "  %s %s  %s\n", nameStyle.Render(t.name), checkMark, msg)
				updated++
			}
			continue
		}
		if updateCheckOnly {
			fmt.Fprintf(cliOut, "  %s %s  source checkout — `missionctl update` pulls and rebuilds\n", nameStyle.Render(t.name), checkMark)
			updated++
			continue
		}

		pull := exec.Command("git", "-C", toolPath, "pull", "--ff-only")
		if out, err := pull.CombinedOutput(); err != nil {
			fmt.Fprintf(cliOut, "  %s %s  git pull failed: %s\n", nameStyle.Render(t.name), crossMark, firstLine(string(out)))
			failed = append(failed, t.name)
			continue
		}

		if err := installTool(t); err != nil {
			fmt.Fprintf(cliOut, "  %s %s  setup.sh failed: %s\n", nameStyle.Render(t.name), crossMark, err)
			failed = append(failed, t.name)
			continue
		}

		fmt.Fprintf(cliOut, "  %s %s  rebuilt & reinstalled\n", nameStyle.Render(t.name), checkMark)
		updated++
	}

	fmt.Fprintln(cliOut)
	verb := "updated"
	if updateCheckOnly {
		verb = "checked"
	}
	fmt.Fprintf(cliOut, "  %d/%d tools %s\n", updated, len(tools), verb)
	fmt.Fprintln(cliOut)

	if len(failed) > 0 {
		return fmt.Errorf("%d tool(s) failed to update: %v", len(failed), failed)
	}
	return nil
}

func firstLine(s string) string {
	before, _, _ := strings.Cut(s, "\n")
	return before
}
