package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// licensedTools are the tools that ship their own `<tool> license`
// subcommand (see each one's cmd/license.go) — the rest of the suite
// (taskctl, timectl, diaryctl) has no Pro gate yet, so there's nothing to
// activate there. Used for `license status`, which is read-only and safe
// to run against every one of them, postctl included.
var licensedTools = []string{"calctl", "budgetctl", "notectl", "habctl", "mailctl", "postctl"}

// bundleTools are the licensedTools actually covered by the missionctl
// Bundle key — postctl is deliberately excluded. It has always been its
// own separate product with its own license (Bundle purchases never
// unlock it), so running a Bundle key against it can only ever 403. Worse
// than pointless: each tool's own `license activate` unconditionally
// saves a failed attempt's result, which was overwriting postctl's real,
// separately-activated key with "invalid" every time `missionctl license
// activate <bundle-key>` ran — silently locking out a working license.
var bundleTools = []string{"calctl", "budgetctl", "notectl", "habctl", "mailctl"}

var licenseCmd = &cobra.Command{
	Use:   "license",
	Short: "Activate your missionctl Bundle license across every tool at once",
	Long: `A Bundle key unlocks every Bundle tool's Pro features. This is a thin
wrapper that runs each installed tool's own "license activate"/"license
status" once, so you enter the key here instead of five separate times.
postctl has its own separate product license and isn't part of the
Bundle — "license status" still reports it, but "license activate" skips
it.`,
}

var licenseActivateCmd = &cobra.Command{
	Use:   "activate <key>",
	Short: "Activate your Bundle license key on every installed tool",
	Args:  cobra.ExactArgs(1),
	RunE:  runLicenseActivate,
}

var licenseStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show each tool's current license status",
	RunE:  runLicenseStatus,
}

func init() {
	licenseCmd.AddCommand(licenseActivateCmd)
	licenseCmd.AddCommand(licenseStatusCmd)
	rootCmd.AddCommand(licenseCmd)
}

func runLicenseActivate(cmd *cobra.Command, args []string) error {
	key := strings.TrimSpace(args[0])

	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("✓")
	crossMark := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("✗")
	dashMark := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("–")
	nameStyle := lipgloss.NewStyle().Width(14)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println()
	results := activateAll(key)
	activated, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch {
		case !r.installed:
			fmt.Printf("  %s %s  not installed, skipped\n", nameStyle.Render(r.tool), dashMark)
			skipped++
		case !r.ok:
			fmt.Printf("  %s %s  %s\n", nameStyle.Render(r.tool), crossMark, detailStyle.Render(r.detail))
			failed++
		default:
			fmt.Printf("  %s %s  activated\n", nameStyle.Render(r.tool), checkMark)
			activated++
		}
	}
	fmt.Println()

	if failed > 0 {
		return fmt.Errorf("%d of %d tool(s) failed to activate — double-check the key and try again", failed, activated+failed)
	}
	if activated == 0 {
		return fmt.Errorf("no licensable tools are installed — run `missionctl doctor` first")
	}
	fmt.Printf("  Bundle unlocked on %d tool(s).\n\n", activated)
	return nil
}

func runLicenseStatus(cmd *cobra.Command, args []string) error {
	nameStyle := lipgloss.NewStyle().Width(14)
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Println()
	for _, r := range statusAll() {
		fmt.Printf("  %s %s\n", nameStyle.Render(r.tool), mutedStyle.Render(r.detail))
	}
	fmt.Println()
	return nil
}

// toolResult is one tool's outcome from activateAll/statusAll — shared
// between the CLI commands above and the dashboard's license settings
// screen (see settings.go) so both drive the exact same per-tool logic
// instead of two copies drifting apart.
type toolResult struct {
	tool      string
	installed bool
	ok        bool
	detail    string
}

// activateAll runs `<tool> license activate <key>` on every installed
// licensedTool.
func activateAll(key string) []toolResult {
	results := make([]toolResult, 0, len(bundleTools))
	for _, name := range bundleTools {
		if _, err := exec.LookPath(name); err != nil {
			results = append(results, toolResult{tool: name})
			continue
		}
		out, err := exec.Command(name, "license", "activate", key).CombinedOutput()
		if err != nil {
			results = append(results, toolResult{tool: name, installed: true, detail: resultLine(string(out), "✗")})
			continue
		}
		results = append(results, toolResult{tool: name, installed: true, ok: true, detail: "activated"})
	}
	return results
}

// statusAll runs `<tool> license status` on every installed licensedTool.
// ok is best-effort here — status doesn't have a clean pass/fail the way
// activate does, so it's set true whenever the command itself ran
// successfully (the detail text carries the actual license type/state).
func statusAll() []toolResult {
	results := make([]toolResult, 0, len(licensedTools))
	for _, name := range licensedTools {
		if _, err := exec.LookPath(name); err != nil {
			results = append(results, toolResult{tool: name, detail: "not installed"})
			continue
		}
		out, err := exec.Command(name, "license", "status").CombinedOutput()
		if err != nil {
			results = append(results, toolResult{tool: name, installed: true, detail: "error: " + resultLine(string(out), "")})
			continue
		}
		results = append(results, toolResult{tool: name, installed: true, ok: true, detail: resultLine(string(out), "License Type:")})
	}
	return results
}

// resultLine picks the single most informative line out of a tool's
// (potentially multi-line) `license activate`/`license status` output —
// the one starting with prefix, or the last non-empty line if prefix
// isn't found (activate's error path prints "✗ Activation failed: ..." as
// its own line; status always prints a "License Type: ..." line first).
func resultLine(out, prefix string) string {
	var lastNonEmpty string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastNonEmpty = line
		if prefix != "" && strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return lastNonEmpty
}
