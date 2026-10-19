package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// The dashboard's health line: things that quietly go wrong and are cheap to notice —
// a tool that pulls from an external source has not synced for days, an Instagram token
// is about to expire. Silence means everything is fine.

const staleSyncAfter = 36 * time.Hour

// staleSyncIssues lists the syncing tools (taskctl, calctl, notectl, mailctl) shown on the dashboard
// whose database has not been written for staleSyncAfter. Tools without a database yet are not listed.
func staleSyncIssues(tools []string, mtime func(tool string) (time.Time, bool), now time.Time) []string {
	var out []string
	for _, t := range tools {
		ts, ok := mtime(t)
		if !ok {
			continue
		}
		if d := now.Sub(ts); d > staleSyncAfter {
			out = append(out, fmt.Sprintf("%s not synced for %s", t, ago(d)))
		}
	}
	return out
}

// conflictCopyIssues reports sync-conflict copies of a tool's database (Dropbox "conflicted copy", Dutch/German
// variants like "in Konflikt stehende Kopie"). They appear when two computers write the same SQLite file and
// are a sign that the real database may be at risk. Only *.db files next to the tool's own database count.
func conflictCopyIssues(tools []string, dbPath func(tool string) string) []string {
	var out []string
	for _, t := range tools {
		path := dbPath(t)
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			continue
		}
		n := 0
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if e.Name() == filepath.Base(path) || !strings.HasSuffix(name, ".db") {
				continue
			}
			if strings.Contains(name, "conflict") || strings.Contains(name, "konflikt") {
				n++
			}
		}
		if n > 0 {
			out = append(out, fmt.Sprintf("%s: %d sync-conflict copies of the database (only one computer should write it at a time)", t, n))
		}
	}
	return out
}

// igTokenIssues reads token_issued of the Instagram-Login token from postctl config files (profile → path).
// Tokens last 60 days and the scheduler refreshes them from day 30, so beyond 45 days something is wrong.
func igTokenIssues(cfgs map[string]string, now time.Time) []string {
	var out []string
	for profile, path := range cfgs {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var c struct {
			Instagram struct {
				API         string `yaml:"api"`
				TokenIssued string `yaml:"token_issued"`
			} `yaml:"instagram"`
		}
		if yaml.Unmarshal(b, &c) != nil || c.Instagram.API != "instagram" || c.Instagram.TokenIssued == "" {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02", c.Instagram.TokenIssued, time.Local)
		if err != nil {
			continue
		}
		age := int(now.Sub(t).Hours() / 24)
		name := "postctl"
		if profile != "" {
			name += " " + profile
		}
		switch {
		case age >= 60:
			out = append(out, fmt.Sprintf("%s: Instagram token expired", name))
		case age > 45:
			out = append(out, fmt.Sprintf("%s: Instagram token expires in %d days (is the scheduler running?)", name, 60-age))
		}
	}
	return out
}

// healthIssues gathers everything for the dashboard line.
func healthIssues(now time.Time) []string {
	var visible []string
	for _, c := range dashboardCards {
		for _, s := range syncableTools {
			if c.tool == s {
				visible = append(visible, s)
			}
		}
	}
	out := staleSyncIssues(visible, func(tool string) (time.Time, bool) {
		info, err := os.Stat(expandHome(resolvedToolDBPath(tool)))
		if err != nil {
			return time.Time{}, false
		}
		return info.ModTime(), true
	}, now)
	out = append(out, conflictCopyIssues(toolDBOrder, func(tool string) string { return expandHome(resolvedToolDBPath(tool)) })...)
	cfgs := map[string]string{}
	if home, err := os.UserHomeDir(); err == nil {
		cfgs[""] = filepath.Join(home, ".config", "postctl", "config.yaml")
		entries, _ := os.ReadDir(filepath.Join(home, ".config", "postctl", "profiles"))
		for _, e := range entries {
			if e.IsDir() {
				cfgs[e.Name()] = filepath.Join(home, ".config", "postctl", "profiles", e.Name(), "config.yaml")
			}
		}
	}
	return append(out, igTokenIssues(cfgs, now)...)
}

// healthLine renders the issues on one line, or "" when all is well.
func healthLine(issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	return "⚠ " + strings.Join(issues, " · ")
}
