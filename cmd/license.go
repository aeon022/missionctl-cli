package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"charm.land/lipgloss/v2"
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
	Use:   "activate [key|-]",
	Short: "Activate your Bundle license key on every installed tool",
	Long:  "Pass the key as the argument, or send it on standard input (no argument, or \"-\") to keep it out of the process list: echo KEY | missionctl license activate",
	Args:  cobra.MaximumNArgs(1),
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
	key, err := licenseKeyArg(args, os.Stdin)
	if err != nil {
		return err
	}

	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Fprintln(cliOut)
	results := activateAll(key)
	activated, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch {
		case !r.installed:
			fmt.Fprintf(cliOut, "  %s %s  not installed, skipped\n", nameStyle.Render(r.tool), dashMark)
			skipped++
		case !r.ok:
			fmt.Fprintf(cliOut, "  %s %s  %s\n", nameStyle.Render(r.tool), crossMark, detailStyle.Render(r.detail))
			failed++
		default:
			fmt.Fprintf(cliOut, "  %s %s  activated\n", nameStyle.Render(r.tool), checkMark)
			activated++
		}
	}
	fmt.Fprintln(cliOut)

	if failed > 0 {
		return fmt.Errorf("%d of %d tool(s) failed to activate — double-check the key and try again", failed, activated+failed)
	}
	if activated == 0 {
		return fmt.Errorf("no licensable tools are installed — run `missionctl doctor` first")
	}
	fmt.Fprintf(cliOut, "  Bundle unlocked on %d tool(s).\n\n", activated)
	return nil
}

func runLicenseStatus(cmd *cobra.Command, args []string) error {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fmt.Fprintln(cliOut)
	for _, r := range statusAll() {
		fmt.Fprintf(cliOut, "  %s %s\n", nameStyle.Render(r.tool), mutedStyle.Render(r.detail))
	}
	fmt.Fprintln(cliOut)
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
		out, err := activateOne(name, key)
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

// licenseKeyArg takes the key from the argument, or from standard input when the argument is "-" or missing.
func licenseKeyArg(args []string, stdin io.Reader) (string, error) {
	if len(args) == 1 && args[0] != "-" {
		return strings.TrimSpace(args[0]), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if key := strings.TrimSpace(line); key != "" {
		return key, nil
	}
	if err != nil {
		return "", fmt.Errorf("no key on standard input: %w", err)
	}
	return "", errors.New("no key on standard input")
}

// activateOne hands the key to one tool. Tools that can read it from standard input get it there, which
// keeps it out of the process list; an older tool binary would take "-" as the key itself and save it
// as invalid, so those still get the key as an argument until they are rebuilt.
func activateOne(tool, key string) ([]byte, error) {
	help, _ := exec.Command(tool, "license", "activate", "--help").CombinedOutput()
	if !strings.Contains(string(help), "standard input") {
		return exec.Command(tool, "license", "activate", key).CombinedOutput()
	}
	c := exec.Command(tool, "license", "activate", "-")
	c.Stdin = strings.NewReader(key + "\n")
	return c.CombinedOutput()
}
