package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaleSyncIssues(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	mtime := func(tool string) (time.Time, bool) {
		switch tool {
		case "taskctl":
			return now.Add(-2 * time.Hour), true
		case "calctl":
			return now.Add(-50 * time.Hour), true
		}
		return time.Time{}, false // notectl has no database yet
	}
	got := staleSyncIssues([]string{"taskctl", "calctl", "notectl"}, mtime, now)
	if len(got) != 1 || !strings.HasPrefix(got[0], "calctl not synced for") {
		t.Errorf("only the stale tool is reported, got %v", got)
	}
}

func TestIGTokenIssues(t *testing.T) {
	now := time.Date(2026, 12, 1, 12, 0, 0, 0, time.Local)
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfgs := map[string]string{
		"fresh":   write("a.yaml", "instagram:\n  api: instagram\n  token_issued: \"2026-11-25\"\n"),
		"old":     write("b.yaml", "instagram:\n  api: instagram\n  token_issued: \"2026-10-10\"\n"),
		"expired": write("c.yaml", "instagram:\n  api: instagram\n  token_issued: \"2026-09-01\"\n"),
		"none":    write("d.yaml", "instagram:\n  api: instagram\n  token_issued: \"\"\n"),
		"fb":      write("e.yaml", "instagram:\n  api: facebook\n  token_issued: \"2026-01-01\"\n"),
	}
	got := strings.Join(igTokenIssues(cfgs, now), "|")
	if !strings.Contains(got, "postctl old: Instagram token expires in") || !strings.Contains(got, "postctl expired: Instagram token expired") || strings.Contains(got, "fresh") || strings.Contains(got, "none") || strings.Contains(got, "fb") {
		t.Errorf("unexpected issues: %q", got)
	}
}

func TestHealthLineIsSilentWhenHealthy(t *testing.T) {
	if healthLine(nil) != "" || !strings.HasPrefix(healthLine([]string{"x"}), "⚠ x") {
		t.Error("no issues → no line; issues → one ⚠ line")
	}
}
