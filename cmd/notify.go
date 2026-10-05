package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/aeon022/missionctl-core/applescript"
	"github.com/spf13/cobra"
)

// notification is one macOS banner. Key identifies it so the same event,
// habit or digest is only ever announced once.
type notification struct{ Key, Title, Body string }

type habitRisk struct {
	Name   string
	Streak int
}

type notifyContext struct {
	Now      time.Time
	Timed    []agendaItem // today's scheduled items
	Habits   []habitRisk  // not checked today, streak > 0
	DueCount int          // tasks due today or overdue
}

const (
	eventLeadTime   = 10 * time.Minute
	habitRiskHour   = 18 // after this, an unchecked habit with a streak is "at risk"
	overdueDigestAt = 9  // one morning digest, not before this hour
	seenRetention   = 48 * time.Hour
)

// decideNotifications is the whole policy, pure: given the current state and
// the keys already announced, which banners should fire now?
func decideNotifications(c notifyContext, seen map[string]time.Time) []notification {
	var out []notification
	add := func(n notification) {
		if _, done := seen[n.Key]; !done {
			out = append(out, n)
		}
	}
	day := c.Now.Format("2006-01-02")

	for _, it := range c.Timed {
		if !it.hasTime {
			continue
		}
		until := it.when.Sub(c.Now)
		if until > -time.Minute && until <= eventLeadTime {
			mins := max(int(until.Round(time.Minute).Minutes()), 0)
			body := fmt.Sprintf("in %d min — %s", mins, it.text)
			if mins == 0 {
				body = "now — " + it.text
			}
			add(notification{Key: "event|" + it.when.Format(time.RFC3339) + "|" + it.text, Title: "Upcoming", Body: body})
		}
	}
	if c.Now.Hour() >= habitRiskHour {
		for _, h := range c.Habits {
			add(notification{Key: "habit|" + day + "|" + h.Name, Title: "Streak at risk",
				Body: fmt.Sprintf("%s — %d-day streak, not checked in today", h.Name, h.Streak)})
		}
	}
	if c.Now.Hour() >= overdueDigestAt && c.DueCount > 0 {
		add(notification{Key: "overdue|" + day, Title: "Tasks",
			Body: fmt.Sprintf("%d task(s) due today or overdue", c.DueCount)})
	}
	return out
}

func pruneSeen(seen map[string]time.Time, now time.Time) {
	for k, t := range seen {
		if now.Sub(t) > seenRetention {
			delete(seen, k)
		}
	}
}

func notifyStatePath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "missionctl", "notified.json")
}

func loadSeen(path string) map[string]time.Time {
	seen := map[string]time.Time{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &seen)
	}
	return seen
}

func saveSeen(path string, seen map[string]time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(seen)
	return os.WriteFile(path, b, 0o644)
}

func gatherNotifyContext(now time.Time) notifyContext {
	c := notifyContext{Now: now}
	_, c.Timed = splitAgendaItems(buildAgendaItems(now))

	var habits struct {
		Data []struct {
			Name         string `json:"name"`
			CheckedToday bool   `json:"checked_today"`
			Streak       int    `json:"streak"`
		} `json:"data"`
	}
	if runToolJSON("habctl", []string{"today", "--json"}, &habits) {
		for _, h := range habits.Data {
			if !h.CheckedToday && h.Streak > 0 {
				c.Habits = append(c.Habits, habitRisk{h.Name, h.Streak})
			}
		}
	}
	var tasks struct {
		Data []any `json:"data"`
	}
	if runToolJSON("taskctl", []string{"list", "--today", "--json"}, &tasks) {
		c.DueCount = len(tasks.Data)
	}
	return c
}

// postBanner shows a macOS notification. Variable so tests don't pop banners.
var postBanner = func(n notification) error {
	script := fmt.Sprintf(`display notification "%s" with title "missionctl" subtitle "%s"`,
		applescript.Escape(n.Body), applescript.Escape(n.Title))
	_, err := applescript.Run(script)
	return err
}

const notifyLabel = "sh.missionctl.notify"

func notifyAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", notifyLabel+".plist")
}

func notifyPlist(self string, interval int, logDir string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>notify</string>
    </array>
    <key>StartInterval</key>
    <integer>%d</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/notify.log</string>
    <key>StandardErrorPath</key>
    <string>%s/notify.log</string>
</dict>
</plist>
`, notifyLabel, self, interval, logDir, logDir)
}

var notifyDry, notifyInstall, notifyUninstall bool

var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Post macOS notifications: event in 10 min, streak at risk (evening), tasks due (morning digest)",
	Long: `Checks your calendar, habits and tasks once and posts a banner for anything that
needs attention. Each banner fires only once (state in the config dir), so it is safe to run
every few minutes — use --install to write a LaunchAgent that does exactly that.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		switch {
		case notifyInstall:
			self, err := os.Executable()
			if err != nil {
				return err
			}
			logDir := filepath.Join(os.Getenv("HOME"), "Library", "Logs", "missionctl")
			if err := os.MkdirAll(logDir, 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(notifyAgentPath()), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(notifyAgentPath(), []byte(notifyPlist(self, 300, logDir)), 0o644); err != nil {
				return err
			}
			fmt.Fprintln(cliOut, "LaunchAgent written:", notifyAgentPath())
			fmt.Fprintln(cliOut, "To activate:  launchctl load", notifyAgentPath())
			return nil
		case notifyUninstall:
			_ = exec.Command("launchctl", "unload", notifyAgentPath()).Run()
			if err := os.Remove(notifyAgentPath()); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Fprintln(cliOut, "LaunchAgent removed.")
			return nil
		}

		now := time.Now()
		path := notifyStatePath()
		seen := loadSeen(path)
		pruneSeen(seen, now)
		due := decideNotifications(gatherNotifyContext(now), seen)
		if len(due) == 0 {
			if notifyDry {
				fmt.Fprintln(cliOut, dashMutedStyle.Render("  Nothing to notify."))
			}
			return nil
		}
		for _, n := range due {
			if notifyDry {
				fmt.Fprintf(cliOut, "  %s  %s\n", dashKeyStyle.Render(n.Title), n.Body)
				continue
			}
			if err := postBanner(n); err != nil {
				fmt.Fprintln(os.Stderr, "notify:", err)
				continue
			}
			seen[n.Key] = now
		}
		if notifyDry {
			return nil
		}
		return saveSeen(path, seen)
	},
}

func init() {
	notifyCmd.Flags().BoolVar(&notifyDry, "dry-run", false, "Show what would be announced; post nothing, remember nothing")
	notifyCmd.Flags().BoolVar(&notifyInstall, "install", false, "Write a LaunchAgent that runs this every 5 minutes")
	notifyCmd.Flags().BoolVar(&notifyUninstall, "uninstall", false, "Unload and remove that LaunchAgent")
	rootCmd.AddCommand(notifyCmd)
}
