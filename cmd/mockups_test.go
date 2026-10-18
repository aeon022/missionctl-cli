package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/activity"
	"github.com/aeon022/missionctl-core/theme"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real dashboard (fictional demo data) for
// the landing page: MISSIONCTL_MOCKUP_DIR=/dir go test ./cmd -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("MISSIONCTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set MISSIONCTL_MOCKUP_DIR to render landing mockups")
	}
	t.Setenv("HOME", t.TempDir())
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasSuffix(k, "_DATA_DIR") {
			t.Setenv(k, "")
		}
	}
	// pretend every tool database was synced two minutes ago (cards show "synced 2m ago")
	for _, rel := range toolDB {
		p := expandHome(rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o600)
		ts := time.Now().Add(-2 * time.Minute)
		_ = os.Chtimes(p, ts, ts)
	}
	saved := dashboardCards
	defer func() { dashboardCards = saved }()
	dashboardCards = filterCards(allDashboardCards, []string{"tasks", "calendar", "timer", "diary", "budget", "habits", "notes", "mail", "postctl"})

	now := time.Now()
	demo := map[string]cardStatus{
		"Tasks":    {text: "3 due today\nPrepare invoice for Nordlicht", spark: []float64{1, 3, 2, 4, 2, 5, 3}},
		"Calendar": {text: "Next: Team standup 09:30\n4 events today"},
		"Timer":    {text: "● Landing page  01:42:10\n4h 20m logged today", spark: []float64{180, 240, 200, 310, 260, 120, 260}},
		"Diary":    {text: "Entry written today\n12 commits · 3 repos"},
		"Budget":   {text: "€1,240 left this month\nGroceries at 82% of budget", urgency: urgencyWarn},
		"Habits":   {text: "4/6 done today\nnext: Read 30 min"},
		"Notes":    {text: "128 notes\nlast: Roadmap Q4"},
		"Mail":     {text: "7 unread\nfrom: Mara Klein"},
		"Postctl": {text: "9 scheduled",
			left:  []string{"tomorrow 17:30 · IG (studio)", "tomorrow 18:00 · Threads (studio)", "Mon 09:00 · LinkedIn (studio)"},
			right: []string{"9 scheduled · 2 drafts", "✓ all clear", "last post 3h ago"}},
	}
	// the same dashboard in a few built-in themes (landing page: live theme switching)
	orig := theme.Current()
	defer func() { _ = theme.Apply(orig) }()
	for _, name := range []string{orig, "nord", "dracula", "gruvbox"} {
		if err := theme.Apply(name); err != nil {
			t.Fatal(err)
		}
		fname := "dashboard-" + name + ".ansi"
		if name == orig {
			fname = "dashboard.ansi"
		}
		m := newDashboardModel()
		m.width, m.height = 130, 44
		vals := make([]cardStatus, len(dashboardCards))
		for i, c := range dashboardCards {
			vals[i] = demo[c.label]
		}
		at := func(h, mi int) time.Time {
			return time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, now.Location())
		}
		ev := []activity.Event{
			{Time: at(8, 40), Tool: "habctl", Action: "checked", Title: "Morning run"},
			{Time: at(9, 5), Tool: "taskctl", Action: "completed", Title: "Reply to agency"},
			{Time: at(9, 31), Tool: "timectl", Action: "started", Title: "Landing page"},
			{Time: at(11, 2), Tool: "notectl", Action: "wrote", Title: "Roadmap Q4"},
			{Time: at(12, 10), Tool: "postctl", Action: "published", Title: "Summer menu — teaser"},
		}
		mi, _ := m.Update(cardsLoadedMsg{values: vals, at: now, activity: ev})
		mi, _ = mi.(dashboardModel).Update(agendaLoadedMsg{
			allDay: []agendaItem{{icon: "📅", color: lipgloss.Color("42"), text: "Offsite planning"}},
			timed: []agendaItem{
				{when: at(9, 30), hasTime: true, icon: "📅", color: lipgloss.Color("42"), text: "Team standup"},
				{when: at(11, 0), hasTime: true, icon: "✓", color: lipgloss.Color("39"), text: "Prepare invoice for Nordlicht"},
				{when: at(14, 0), hasTime: true, icon: "📅", color: lipgloss.Color("42"), text: "Client call — redesign"},
				{when: at(16, 30), hasTime: true, icon: "⏱", color: lipgloss.Color("221"), text: "Focus block"},
			},
		})
		out := mi.(dashboardModel).View().Content
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
