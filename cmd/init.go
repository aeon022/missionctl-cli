package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive setup wizard",
	RunE:  runInit,
}

func runInit(_ *cobra.Command, _ []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Fprintln(cliOut)
	fmt.Fprintf(cliOut, "  %s\n", headerStyle.Render("missionctl — Setup Wizard"))
	fmt.Fprintln(cliOut)

	exports := []string{}

	// [1/3] ANTHROPIC_API_KEY
	fmt.Fprintf(cliOut, "  %s\n", promptStyle.Render("[1/3] ANTHROPIC_API_KEY"))
	current := os.Getenv("ANTHROPIC_API_KEY")
	if current != "" {
		fmt.Fprintf(cliOut, "  Already set.\n")
	} else {
		fmt.Fprintf(cliOut, "  %s\n", dimStyle.Render("Not set. AI features (diary auto-fill, meeting summary) require this."))
		fmt.Fprintf(cliOut, "  Enter key (or press Enter to skip): ")
		apiKey := ""
		if scanner.Scan() {
			apiKey = strings.TrimSpace(scanner.Text())
		}
		if apiKey != "" {
			exports = append(exports, fmt.Sprintf("export ANTHROPIC_API_KEY=%s", apiKey))
		}
	}

	fmt.Fprintln(cliOut)

	// [2/3] Obsidian Vault Path
	fmt.Fprintf(cliOut, "  %s\n", promptStyle.Render("[2/3] Obsidian Vault Path (for notectl)"))
	fmt.Fprintf(cliOut, "  Enter path (or press Enter to skip): ")
	vaultPath := ""
	if scanner.Scan() {
		vaultPath = strings.TrimSpace(scanner.Text())
	}
	if vaultPath != "" {
		exports = append(exports, fmt.Sprintf("export NOTECTL_VAULT_PATH=%s", vaultPath))
	}

	fmt.Fprintln(cliOut)

	// [3/3] Missing tools
	missingTools := []string{}
	for _, t := range tools {
		if _, err := exec.LookPath(t.name); err != nil {
			missingTools = append(missingTools, t.name)
		}
	}

	if len(missingTools) > 0 {
		fmt.Fprintf(cliOut, "  %s\n", promptStyle.Render(fmt.Sprintf("[3/3] Tools not found: %s", strings.Join(missingTools, ", "))))
		fmt.Fprintf(cliOut, "  Install now? [y/N]: ")
		answer := ""
		if scanner.Scan() {
			answer = strings.TrimSpace(scanner.Text())
		}
		if strings.ToLower(answer) == "y" {
			for _, t := range tools {
				if _, err := exec.LookPath(t.name); err != nil {
					fmt.Fprintf(cliOut, "\n  Installing %s...\n", t.name)
					if err := installTool(t); err != nil {
						fmt.Fprintf(cliOut, "  Failed to install %s: %v\n", t.name, err)
					}
				}
			}
		}
	} else {
		fmt.Fprintf(cliOut, "  %s\n", promptStyle.Render("[3/3] All tools installed"))
	}

	fmt.Fprintln(cliOut)
	fmt.Fprintf(cliOut, "  %s\n", headerStyle.Render("Done."))

	if len(exports) > 0 {
		fmt.Fprintf(cliOut, "  Add to your shell profile:\n")
		for _, line := range exports {
			fmt.Fprintf(cliOut, "    %s\n", dimStyle.Render(line))
		}
	}

	fmt.Fprintf(cliOut, "  Run %s to verify.\n", dimStyle.Render("`missionctl doctor`"))
	fmt.Fprintln(cliOut)

	return nil
}
