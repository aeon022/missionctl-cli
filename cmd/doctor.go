package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

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
	checkMCPConfig(checkMark, crossMark, nameStyle, pathStyle)

	fmt.Println()
	checkDatabases(checkMark, dashMark, nameStyle, pathStyle)

	fmt.Println()
	checkDaemons(checkMark, crossMark, dashMark, nameStyle, pathStyle)

	fmt.Println()

	if len(missing) > 0 {
		return fmt.Errorf("%d tool(s) not found: %v", len(missing), missing)
	}
	return nil
}

// mcpTools lists every tool that ships an `<tool> mcp` subcommand (all of
// `tools` except missionctl itself, which has no MCP server).
var mcpTools = []string{"mailctl", "calctl", "taskctl", "notectl", "budgetctl", "postctl", "diaryctl", "timectl", "habctl"}

func checkMCPConfig(checkMark, crossMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Println("  MCP registration (~/.claude.json):")
	fmt.Println()

	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("  %s  %s\n", crossMark, pathStyle.Render("~/.claude.json not found — no MCP servers registered yet"))
		return
	}

	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
		Projects   map[string]struct {
			McpServers map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Printf("  %s  %s\n", crossMark, pathStyle.Render("~/.claude.json could not be parsed: "+err.Error()))
		return
	}

	// `claude mcp add` defaults to project scope, storing the server under
	// projects[cwd].mcpServers rather than the top-level (user-scope)
	// mcpServers — checking only the top level made every project-scoped
	// registration look "not registered" even though it works fine.
	registered := map[string]bool{}
	for name := range cfg.McpServers {
		registered[name] = true
	}
	if cwd, err := os.Getwd(); err == nil {
		if proj, ok := cfg.Projects[cwd]; ok {
			for name := range proj.McpServers {
				registered[name] = true
			}
		}
	}

	for _, name := range mcpTools {
		if registered[name] {
			fmt.Printf("  %s %s  registered\n", nameStyle.Render(name), checkMark)
		} else {
			fmt.Printf("  %s %s  not registered — add: %s\n",
				nameStyle.Render(name), crossMark,
				pathStyle.Render(fmt.Sprintf(`claude mcp add %s -- %s mcp`, name, name)))
		}
	}
}

// toolDB maps a tool name to its on-disk SQLite database path. postctl is
// intentionally excluded — its work is deferred, see POSTCTL_AUDIT.md.
var toolDB = map[string]string{
	"mailctl":   "~/Library/Application Support/mailctl/mailctl.db",
	"calctl":    "~/Library/Application Support/calctl/calctl.db",
	"taskctl":   "~/Library/Application Support/taskctl/taskctl.db",
	"notectl":   "~/.local/share/notectl/notes.db",
	"budgetctl": "~/.local/share/budgetctl/budget.db",
	"habctl":    "~/.local/share/habctl/habits.db",
	"timectl":   "~/.local/share/timectl/time.db",
	"diaryctl":  "~/.local/share/diaryctl/diary.db",
}

// toolDBEnvPrefix lists the tools whose own config supports redirecting
// their data directory away from the private default in toolDB via a
// <PREFIX>_DATA_DIR env var (e.g. NOTECTL_DATA_DIR pointing notectl's DB
// into a Dropbox-synced folder — see notectl/internal/config.DBPath and
// each other tool's own internal/config or internal/store package for the
// same convention). All eight tools that ship a DB support it; confirmed
// against each one's own source, not just this repo's env var usage.
var toolDBEnvPrefix = map[string]string{
	"mailctl":   "MAILCTL",
	"calctl":    "CALCTL",
	"taskctl":   "TASKCTL",
	"notectl":   "NOTECTL",
	"budgetctl": "BUDGETCTL",
	"habctl":    "HABCTL",
	"timectl":   "TIMECTL",
	"diaryctl":  "DIARYCTL",
}

// resolvedToolDBPath returns a tool's actual on-disk DB path, honoring its
// own <PREFIX>_DATA_DIR override when set instead of assuming toolDB's
// private-default path always applies.
func resolvedToolDBPath(tool string) string {
	defaultPath := toolDB[tool]
	if prefix, ok := toolDBEnvPrefix[tool]; ok {
		if dir := os.Getenv(prefix + "_DATA_DIR"); dir != "" {
			return filepath.Join(expandHome(dir), filepath.Base(defaultPath))
		}
	}
	return defaultPath
}

// toolDBOrder keeps the database status output in a stable, readable order.
var toolDBOrder = []string{"mailctl", "calctl", "taskctl", "notectl", "budgetctl", "habctl", "timectl", "diaryctl"}

func checkDatabases(checkMark, dashMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Println("  Databases:")
	fmt.Println()

	for _, name := range toolDBOrder {
		path := expandHome(resolvedToolDBPath(name))
		info, err := os.Stat(path)
		if err != nil {
			fmt.Printf("  %s %s  not created yet\n", nameStyle.Render(name), dashMark)
			continue
		}
		age := time.Since(info.ModTime())
		fmt.Printf("  %s %s  last synced %s\n", nameStyle.Render(name), checkMark, pathStyle.Render(formatAge(age)+" ago"))
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

type daemon struct {
	tool  string
	label string
}

// Only diaryctl and taskctl ship a launchd daemon today.
var daemons = []daemon{
	{"diaryctl", "sh.missionctl.diaryctl"},
	{"taskctl", "com.taskctl.daemon"},
}

func checkDaemons(checkMark, crossMark, dashMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Println("  launchd daemons:")
	fmt.Println()

	home, _ := os.UserHomeDir()
	for _, d := range daemons {
		plistPath := filepath.Join(home, "Library", "LaunchAgents", d.label+".plist")
		if _, err := os.Stat(plistPath); err != nil {
			fmt.Printf("  %s %s  not installed — see `%s daemon --install`\n", nameStyle.Render(d.tool), dashMark, d.tool)
			continue
		}
		if err := exec.Command("launchctl", "list", d.label).Run(); err != nil {
			fmt.Printf("  %s %s  plist exists but not loaded — try `launchctl load -w %s`\n",
				nameStyle.Render(d.tool), crossMark, pathStyle.Render(plistPath))
			continue
		}
		fmt.Printf("  %s %s  loaded\n", nameStyle.Render(d.tool), checkMark)
	}
}
