package cmd

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestDashboardThemeKeySwitchesLiveAndSaves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newDashboardModel()
	before := dashKeyStyle.GetForeground()
	mi, _ := m.Update(tea.KeyPressMsg{Text: "T", Code: 'T'})

	if theme.Current() == orig {
		t.Fatalf("T must switch to the next theme, still %q", orig)
	}
	if dashKeyStyle.GetForeground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if got := mi.(dashboardModel).actionMsg; got != "Theme: "+theme.Current() {
		t.Errorf("a short confirmation should name the theme, got %q", got)
	}
}
