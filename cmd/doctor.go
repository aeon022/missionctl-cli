package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check which tools are installed and environment variables set",
	RunE:  runDoctor,
}

type tool struct {
	name    string
	project string // subfolder under ~/Developing/Projects/missionctl/
}

var tools = []tool{
	{"mailctl", "mailctl"},
	{"calctl", "calctl"},
	{"taskctl", "taskctl"},
	{"notectl", "notectl"},
	{"budgetctl", "budgetctl"},
	{"postctl", "postctl"},
	{"diaryctl", "diaryctl"},
	{"timectl", "timectl"},
	{"habctl", "habctl"},
}

type envVar struct {
	name     string
	optional bool
}

var envVars = []envVar{
	{"ANTHROPIC_API_KEY", false},
	{"TIMECTL_GOAL_HOURS", true},
	{"TIMECTL_HOURLY_RATE", true},
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("✓")
	crossMark := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("✗")
	dashMark := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("–")

	nameStyle := lipgloss.NewStyle().Width(14)
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	labelStyle := lipgloss.NewStyle().Width(22)

	fmt.Println()

	found := 0
	missing := []string{}

	for _, t := range tools {
		path, err := exec.LookPath(t.name)
		if err == nil {
			fmt.Printf("  %s %s  %s\n",
				nameStyle.Render(t.name),
				checkMark,
				pathStyle.Render(path),
			)
			found++
		} else {
			installCmd := fmt.Sprintf("bash ~/Developing/Projects/missionctl/%s/setup.sh", t.project)
			fmt.Printf("  %s %s  not found — install: %s\n",
				nameStyle.Render(t.name),
				crossMark,
				pathStyle.Render(installCmd),
			)
			missing = append(missing, t.name)
		}
	}

	fmt.Println()
	total := len(tools)
	fmt.Printf("  %d/%d tools installed\n", found, total)

	fmt.Println()

	for _, ev := range envVars {
		val := os.Getenv(ev.name)
		if val != "" {
			fmt.Printf("  %s %s  set\n", labelStyle.Render(ev.name), checkMark)
		} else if ev.optional {
			fmt.Printf("  %s %s  not set (optional)\n", labelStyle.Render(ev.name), dashMark)
		} else {
			fmt.Printf("  %s %s  not set\n", labelStyle.Render(ev.name), crossMark)
		}
	}

	fmt.Println()

	if len(missing) > 0 {
		return fmt.Errorf("%d tool(s) not found: %v", len(missing), missing)
	}
	return nil
}
