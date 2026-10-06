package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
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
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	labelStyle := lipgloss.NewStyle().Width(22)

	fmt.Fprintln(cliOut)

	found := 0
	missing := []string{}

	for _, t := range tools {
		path, err := exec.LookPath(t.name)
		if err == nil {
			fmt.Fprintf(cliOut, "  %s %s  %s\n",
				nameStyle.Render(t.name),
				checkMark,
				pathStyle.Render(path),
			)
			found++
		} else {
			installCmd := fmt.Sprintf("bash ~/Developing/Projects/missionctl/%s/setup.sh", t.project)
			fmt.Fprintf(cliOut, "  %s %s  not found — install: %s\n",
				nameStyle.Render(t.name),
				crossMark,
				pathStyle.Render(installCmd),
			)
			missing = append(missing, t.name)
		}
	}

	fmt.Fprintln(cliOut)
	total := len(tools)
	fmt.Fprintf(cliOut, "  %d/%d tools installed\n", found, total)

	fmt.Fprintln(cliOut)

	for _, ev := range envVars {
		val := os.Getenv(ev.name)
		if val != "" {
			fmt.Fprintf(cliOut, "  %s %s  set\n", labelStyle.Render(ev.name), checkMark)
		} else if ev.optional {
			fmt.Fprintf(cliOut, "  %s %s  not set (optional)\n", labelStyle.Render(ev.name), dashMark)
		} else {
			fmt.Fprintf(cliOut, "  %s %s  not set\n", labelStyle.Render(ev.name), crossMark)
		}
	}

	fmt.Fprintln(cliOut)
	checkMCPConfig(checkMark, crossMark, nameStyle, pathStyle)

	fmt.Fprintln(cliOut)
	checkDatabases(checkMark, dashMark, nameStyle, pathStyle)

	fmt.Fprintln(cliOut)
	checkToolSelfTests(checkMark, crossMark, dashMark, nameStyle, pathStyle)

	fmt.Fprintln(cliOut)
	checkDaemons(checkMark, crossMark, dashMark, nameStyle, pathStyle)

	fmt.Fprintln(cliOut)

	if len(missing) > 0 {
		return fmt.Errorf("%d tool(s) not found: %v", len(missing), missing)
	}
	return nil
}

// mcpTools lists every tool that ships an `<tool> mcp` subcommand — every
// entry in `tools` (missionctl itself has no MCP server and isn't in that
// list) — derived directly instead of kept as a second hand-written copy
// that could drift out of sync with it.
var mcpTools = func() []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.name
	}
	return names
}()

func checkMCPConfig(checkMark, crossMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Fprintln(cliOut, "  MCP registration (~/.claude.json):")
	fmt.Fprintln(cliOut)

	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(cliOut, "  %s  %s\n", crossMark, pathStyle.Render("~/.claude.json not found — no MCP servers registered yet"))
		return
	}

	var cfg struct {
		McpServers map[string]any `json:"mcpServers"`
		Projects   map[string]struct {
			McpServers map[string]any `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(cliOut, "  %s  %s\n", crossMark, pathStyle.Render("~/.claude.json could not be parsed: "+err.Error()))
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
			fmt.Fprintf(cliOut, "  %s %s  registered\n", nameStyle.Render(name), checkMark)
		} else {
			fmt.Fprintf(cliOut, "  %s %s  not registered — add: %s\n",
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

// resolvedToolDBPath returns a tool's actual on-disk DB path, honoring its
// own <PREFIX>_DATA_DIR override when set instead of assuming toolDB's
// private-default path always applies (e.g. NOTECTL_DATA_DIR pointing
// notectl's DB into a Dropbox-synced folder — see notectl/internal/config.DBPath
// and each other tool's own internal/config or internal/store package for
// the same convention). All eight tools that ship a DB support it, and each
// one's prefix is just its name uppercased; confirmed against each one's
// own source, not just this repo's env var usage.
func resolvedToolDBPath(tool string) string {
	defaultPath := toolDB[tool]
	if dir := os.Getenv(strings.ToUpper(tool) + "_DATA_DIR"); dir != "" {
		return filepath.Join(expandHome(dir), filepath.Base(defaultPath))
	}
	return defaultPath
}

// toolDBOrder keeps the database status output in a stable, readable order.
var toolDBOrder = []string{"mailctl", "calctl", "taskctl", "notectl", "budgetctl", "habctl", "timectl", "diaryctl"}

func checkDatabases(checkMark, dashMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Fprintln(cliOut, "  Databases:")
	fmt.Fprintln(cliOut)

	for _, name := range toolDBOrder {
		path := expandHome(resolvedToolDBPath(name))
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(cliOut, "  %s %s  not created yet\n", nameStyle.Render(name), dashMark)
			continue
		}
		age := time.Since(info.ModTime())
		line := fmt.Sprintf("  %s %s  last synced %s", nameStyle.Render(name), checkMark, pathStyle.Render(ago(age)))
		if res := sqliteQuickCheck(path); res != "ok" {
			line = fmt.Sprintf("  %s %s  %s", nameStyle.Render(name), crossMark, "integrity check: "+res)
		}
		fmt.Fprintln(cliOut, line)
	}
}

// sqliteQuickCheck runs PRAGMA quick_check on a database opened read-only and
// returns "ok" or the first problem SQLite reports (or why it couldn't check).
func sqliteQuickCheck(path string) string {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err.Error()
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&res); err != nil {
		return err.Error()
	}
	return res
}

// runToolDoctor is variable so tests don't need the real tools installed.
var runToolDoctor = func(ctx context.Context, tool string) ([]byte, error) {
	return exec.CommandContext(ctx, tool, "doctor").CombinedOutput()
}

// toolDoctorVerdict runs `<tool> doctor` (5 s limit) and reduces it to
// installed / ok / the first failing line. A tool is failing when it exits
// non-zero or prints a ✗ line; the ✗ line (else the first output line) is
// the reason shown.
func toolDoctorVerdict(tool string) (installed, ok bool, reason string) {
	if _, err := exec.LookPath(tool); err != nil {
		return false, false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runToolDoctor(ctx, tool)
	var first string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if first == "" {
			first = l
		}
		if strings.HasPrefix(l, "✗") {
			return true, false, l
		}
	}
	if ctx.Err() != nil {
		return true, false, "timed out after 5s"
	}
	if err != nil {
		if first == "" {
			first = err.Error()
		}
		return true, false, first
	}
	return true, true, ""
}

func checkToolSelfTests(checkMark, crossMark, dashMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Fprintln(cliOut, "  Tool self-checks (<tool> doctor):")
	fmt.Fprintln(cliOut)
	for _, name := range toolDBOrder {
		installed, ok, reason := toolDoctorVerdict(name)
		switch {
		case !installed:
			fmt.Fprintf(cliOut, "  %s %s  not installed\n", nameStyle.Render(name), dashMark)
		case ok:
			fmt.Fprintf(cliOut, "  %s %s  all checks passed\n", nameStyle.Render(name), checkMark)
		default:
			fmt.Fprintf(cliOut, "  %s %s  %s\n", nameStyle.Render(name), crossMark, pathStyle.Render(reason))
		}
	}
}

// ago renders how long ago something happened: "just now" (never "just now
// ago"), "5m ago", "2h ago", "3d ago".
func ago(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return formatAge(d) + " ago"
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

// daemonStatus is what `launchctl list <label>` tells us about a loaded
// job — parsed out of its plain property-list-style text dump (no `-json`
// output flag on this launchctl subcommand across the macOS versions this
// needs to run on, so text parsing it is).
type daemonStatus struct {
	pid            string // "" if the job isn't currently running
	lastExitStatus int
	onDemand       bool
}

var (
	pidRe      = regexp.MustCompile(`"PID"\s*=\s*(\d+);`)
	exitRe     = regexp.MustCompile(`"LastExitStatus"\s*=\s*(-?\d+);`)
	onDemandRe = regexp.MustCompile(`"OnDemand"\s*=\s*(true|false);`)
)

func parseDaemonStatus(out string) daemonStatus {
	var s daemonStatus
	if m := pidRe.FindStringSubmatch(out); m != nil {
		s.pid = m[1]
	}
	if m := exitRe.FindStringSubmatch(out); m != nil {
		s.lastExitStatus, _ = strconv.Atoi(m[1])
	}
	if m := onDemandRe.FindStringSubmatch(out); m != nil {
		s.onDemand = m[1] == "true"
	}
	return s
}

// checkDaemons reports each daemon's actual liveness, not just whether
// launchd knows about it — "loaded" used to be the final word here even
// for a continuous daemon (OnDemand=false, meant to always have a PID)
// that had silently died and launchd hadn't respawned, or one whose last
// run had failed outright. Both looked identical to "loaded" before: this
// is exactly the gap that made postctl's missed scheduled posts (no daemon
// installed at all, in that case) take manual digging to diagnose instead
// of showing up here directly.
func checkDaemons(checkMark, crossMark, dashMark string, nameStyle, pathStyle lipgloss.Style) {
	fmt.Fprintln(cliOut, "  launchd daemons:")
	fmt.Fprintln(cliOut)

	home, _ := os.UserHomeDir()
	for _, d := range daemons {
		plistPath := filepath.Join(home, "Library", "LaunchAgents", d.label+".plist")
		if _, err := os.Stat(plistPath); err != nil {
			fmt.Fprintf(cliOut, "  %s %s  not installed — see `%s daemon --install`\n", nameStyle.Render(d.tool), dashMark, d.tool)
			continue
		}
		out, err := exec.Command("launchctl", "list", d.label).Output()
		if err != nil {
			fmt.Fprintf(cliOut, "  %s %s  plist exists but not loaded — try `launchctl load -w %s`\n",
				nameStyle.Render(d.tool), crossMark, pathStyle.Render(plistPath))
			continue
		}

		status := parseDaemonStatus(string(out))
		switch {
		case status.lastExitStatus != 0:
			fmt.Fprintf(cliOut, "  %s %s  last run failed (exit %d) — check its log\n", nameStyle.Render(d.tool), crossMark, status.lastExitStatus)
		case !status.onDemand && status.pid == "":
			fmt.Fprintf(cliOut, "  %s %s  loaded but not running — should be continuous, try `launchctl kickstart -k gui/$(id -u)/%s`\n",
				nameStyle.Render(d.tool), crossMark, d.label)
		case status.pid != "":
			fmt.Fprintf(cliOut, "  %s %s  running (pid %s)\n", nameStyle.Render(d.tool), checkMark, status.pid)
		default:
			fmt.Fprintf(cliOut, "  %s %s  scheduled, last run OK\n", nameStyle.Render(d.tool), checkMark)
		}
	}
}
