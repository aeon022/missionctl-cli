package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/aeon022/missionctl-core/activity"
	"github.com/charmbracelet/x/ansi"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 30, 0, 0, time.Local)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.Local) }
	cases := []struct {
		in   string
		want time.Time
	}{
		{"", day(6)}, {"today", day(6)}, {"Yesterday", day(5)},
		{"1d", day(6)}, {"3d", day(4)}, {"2w", day(6).AddDate(0, 0, -13)},
		{"2026-10-01", day(1)},
	}
	for _, c := range cases {
		got, err := parseSince(c.in, now)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"0d", "xd", "last week", "2026-13-40"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) should fail", bad)
		}
	}
}

func TestFilterEvents(t *testing.T) {
	evs := []activity.Event{{Tool: "taskctl"}, {Tool: "habctl"}, {Tool: "taskctl"}}
	if got := filterEvents(evs, "taskctl"); len(got) != 2 {
		t.Errorf("tool filter: %v", got)
	}
	if got := filterEvents(evs, ""); len(got) != 3 {
		t.Errorf("no filter: %v", got)
	}
}

func TestRenderLogGroupsNewestDayFirst(t *testing.T) {
	now := time.Date(2026, 10, 6, 18, 0, 0, 0, time.Local)
	evs := []activity.Event{
		{Time: now.AddDate(0, 0, -1).Add(-2 * time.Hour), Tool: "habctl", Action: "checked", Title: "Sport"},
		{Time: now.Add(-3 * time.Hour), Tool: "taskctl", Action: "completed", Title: "Steuer"},
		{Time: now.Add(-1 * time.Hour), Tool: "timectl", Action: "started", Title: "Rechnung"},
	}
	out := ansi.Strip(renderLog(evs, now))
	if strings.Index(out, "Today · 2026-10-06") > strings.Index(out, "Yesterday · 2026-10-05") ||
		!strings.Contains(out, "Today · 2026-10-06") || !strings.Contains(out, "Yesterday · 2026-10-05") {
		t.Errorf("days must be newest first:\n%s", out)
	}
	if strings.Index(out, "Steuer") > strings.Index(out, "Rechnung") {
		t.Errorf("within a day oldest first:\n%s", out)
	}
	if !strings.Contains(out, "15:00") || !strings.Contains(out, "taskctl") || !strings.Contains(out, "completed") {
		t.Errorf("columns missing:\n%s", out)
	}
}

func TestRenderLogEmptyMentionsDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	if out := ansi.Strip(renderLog(nil, time.Now())); !strings.Contains(out, "Nothing logged.") {
		t.Errorf("empty: %q", out)
	}
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	if out := ansi.Strip(renderLog(nil, time.Now())); !strings.Contains(out, "logging is off") {
		t.Errorf("disabled hint missing: %q", out)
	}
}
