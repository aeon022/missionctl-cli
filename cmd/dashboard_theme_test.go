package cmd

import (
	"testing"
	"time"

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

func TestInvestAndPostctlCardsHaveKeys(t *testing.T) {
	for _, c := range allDashboardCards {
		if c.key == "" {
			t.Errorf("card %q has no jump key", c.label)
		}
	}
}

func TestClickingTodayPanelOpensAgenda(t *testing.T) {
	m := newDashboardModel()
	m.width, m.height = 130, 60
	mi, _ := m.Update(cardsLoadedMsg{values: make([]cardStatus, len(dashboardCards)), at: time.Now()})
	m = mi.(dashboardModel)
	// find a cell inside the Today panel by scanning down the left column
	hit := -1
	for y := 0; y < m.height; y++ {
		if m.inTodayPanel(m.gridLeft()+2, y) {
			hit = y
			break
		}
	}
	if hit < 0 {
		t.Fatal("no Today panel found at 130x60")
	}
	mi, cmd := m.Update(tea.MouseClickMsg{X: m.gridLeft() + 2, Y: hit, Button: tea.MouseLeft})
	if !mi.(dashboardModel).showAgenda || cmd == nil {
		t.Error("clicking the Today panel should open the agenda and load it")
	}
}
