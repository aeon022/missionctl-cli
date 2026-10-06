package cmd

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderUIDemoShowsEveryBlockWithinWidth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_ICONS", "")
	now := time.Date(2026, 10, 6, 9, 30, 0, 0, time.Local)
	out := renderUIDemo("current theme", now)
	text := ansi.Strip(out)
	for _, want := range []string{"missionctl ui", "09:30", "overdue", "enter", "Groceries", "115%", "▌", "Toasts",
		"Saved 3 transactions", "yesterday", "3d ago", "Spotify", "…", "+3,200.00", "✓", "Nerd Font:", "╭─ Tasks", "Insights", "POSTS 12", "1h 05m", "borders: rounded"} {
		if !strings.Contains(text, want) {
			t.Errorf("demo missing %q:\n%s", want, text)
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > demoWidth {
			t.Errorf("line %d is %d cells wide, max %d: %q", i, w, demoWidth, ansi.Strip(line))
		}
	}
}

func TestDemoPresetsExistInCore(t *testing.T) {
	if len(demoPresets) != 8 {
		t.Errorf("expected the 8 shipped presets, got %v", demoPresets)
	}
}
