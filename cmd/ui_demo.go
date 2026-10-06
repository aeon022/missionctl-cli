package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/spf13/cobra"
)

// demoPresets are the theme presets shipped in missionctl-core/theme.
var demoPresets = []string{"catppuccin", "dracula", "gruvbox", "nord", "one-dark", "solarized", "tokyo-night", "terminal"}

const demoWidth = 72

// renderUIDemo draws every ui building block once, in the current theme.
func renderUIDemo(title string, now time.Time) string {
	var b strings.Builder
	sec := func(name string) { fmt.Fprintf(&b, "\n%s\n", ui.Divider(demoWidth, name)) }

	b.WriteString(ui.Header(demoWidth, "missionctl ui", title, now.Format("15:04")) + "\n")

	sec("Pills & dots")
	b.WriteString(ui.Pill("overdue", ui.Err) + " " + ui.Pill("today", ui.Warn) + " " + ui.Pill("done", ui.OK) +
		" " + ui.Pill("P1", ui.Info) + " " + ui.Pill("draft", ui.Muted) + "   " +
		ui.Dot(ui.OK) + " online  " + ui.Dot(ui.Warn) + " syncing  " + ui.Dot(ui.Err) + " failed\n")

	sec("Key caps")
	b.WriteString(ui.Hint("enter", "open") + "  " + ui.Hint("x", "action") + "  " + ui.Hint("/", "search") + "  " +
		ui.Hint("?", "help") + "  " + ui.Hint("q", "quit") + "\n")

	sec("Bars — budget goals (worse when full) and progress")
	for _, r := range []struct {
		name  string
		ratio float64
	}{{"Groceries", 0.35}, {"Dining", 0.82}, {"Transport", 1.15}} {
		fmt.Fprintf(&b, "%-12s %s %3.0f%%\n", r.name, ui.Bar(30, r.ratio, true), r.ratio*100)
	}
	fmt.Fprintf(&b, "%-12s %s %3.0f%%\n", "Import", ui.Bar(30, 0.666, false), 66.6)

	sec("Sparkline & heatmap")
	b.WriteString("minutes/day  " + ui.Spark([]float64{40, 95, 0, 120, 60, 180, 30}) + "     ")
	b.WriteString("habit  ")
	for _, l := range []int{0, 1, 4, 2, 4, 3, 0, 1, 4, 4, 2, 0, 3, 4} {
		b.WriteString(ui.Heat(l, 4))
	}
	b.WriteString("\n")

	sec("Toasts")
	b.WriteString(ui.Toast(ui.OK, "Saved 3 transactions") + "   " + ui.Toast(ui.Warn, "2 uncategorized") + "\n")
	b.WriteString(ui.Toast(ui.Err, "Sync failed: timeout") + "   " + ui.Toast(ui.Info, "Press ? for help") + "\n")

	sec("List with selection, relative dates, money")
	rows := []struct {
		when  time.Time
		title string
		amt   float64
	}{
		{now, "Gehalt Oktober", 3200}, {now.AddDate(0, 0, -1), "REWE Markt", -64.30},
		{now.AddDate(0, 0, -3), "Miete", -980}, {now.AddDate(0, 0, 2), "Spotify (upcoming)", -10.99},
		{now.AddDate(0, 0, -20), "Rechnung_Steuerberater_Q3_2026_final_v2.pdf", -1450},
	}
	for i, r := range rows {
		line := fmt.Sprintf("%-10s %-30s %s", ui.RelTime(r.when, now), ui.MidEllipsis(r.title, 30), ui.Money(r.amt, 12))
		b.WriteString(ui.Row(demoWidth, i == 1, line) + "\n")
	}

	sec("Panels (title in the border; focused = accent), tabs, durations")
	left := ui.Panel(34, 6, "Tasks", ui.Row(30, true, "buy milk  "+ui.Pill("today", ui.Warn))+"\n"+ui.Row(30, false, "call mom"), true)
	right := ui.Panel(34, 6, "Insights", "Groceries "+ui.Bar(14, 0.62, true)+"\nDining    "+ui.Bar(14, 0.91, true), false)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right) + "\n")
	b.WriteString(ui.Tabs(demoWidth, []string{"DASHBOARD", "POSTS", "QUEUE", "HISTORY", "STATS"}, 1, []int{0, 12, 3}) + "\n")
	fmt.Fprintf(&b, "%s  %s  %s  %s  %s\n", ui.Duration(45*time.Second), ui.Duration(12*time.Minute),
		ui.Duration(2*time.Hour), ui.Duration(65*time.Minute), ui.Duration(27*time.Hour))
	fmt.Fprintf(&b, "borders: %s (MISSIONCTL_BORDERS=rounded|sharp|none)\n", ui.Borders())

	sec("Icons")
	b.WriteString("unicode: ")
	for _, n := range []string{"task", "calendar", "timer", "diary", "budget", "habit", "note", "mail", "search"} {
		b.WriteString(ui.Icon(n) + " ")
	}
	if ui.NerdIcons() {
		b.WriteString("  (Nerd Font set active)")
	} else {
		b.WriteString("  (Nerd Font: MISSIONCTL_ICONS=nerd)")
	}
	b.WriteString("\n")
	return b.String()
}

var uiDemoAll bool
var uiDemoTitle string

var uiDemoCmd = &cobra.Command{
	Use:   "ui-demo",
	Short: "Show every shared UI building block (pills, bars, key caps, …) in your theme",
	Long: `Draws the suite's shared visual vocabulary (missionctl-core/ui) once, so you can judge
colors and shapes in your terminal and theme. With --all it renders every theme preset in turn.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if !uiDemoAll {
			title := uiDemoTitle
			if title == "" {
				title = "current theme"
			}
			fmt.Fprint(cliOut, renderUIDemo(title, time.Now()))
			return nil
		}
		self, err := os.Executable()
		if err != nil {
			return err
		}
		for _, p := range demoPresets {
			home, err := os.MkdirTemp("", "ui-demo-*")
			if err != nil {
				return err
			}
			cfg := filepath.Join(home, ".config", "missionctl")
			_ = os.MkdirAll(cfg, 0o755)
			_ = os.WriteFile(filepath.Join(cfg, "theme.yaml"), []byte("preset: "+p+"\n"), 0o644)
			// Re-run ourselves with HOME pointing at a throwaway theme.yaml: the
			// theme is read once at startup, so each preset needs its own process.
			c := exec.Command(self, "ui-demo", "--title", "theme: "+p)
			c.Env = append(os.Environ(), "HOME="+home)
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			err = c.Run()
			_ = os.RemoveAll(home)
			if err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	uiDemoCmd.Flags().BoolVar(&uiDemoAll, "all", false, "Render every theme preset in turn")
	uiDemoCmd.Flags().StringVar(&uiDemoTitle, "title", "", "Label shown in the header (used by --all)")
	_ = uiDemoCmd.Flags().MarkHidden("title")
	rootCmd.AddCommand(uiDemoCmd)
}
