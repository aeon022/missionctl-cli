package cmd

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestDashboardThemeKeyOpensPickerWithLivePreview(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newDashboardModel()
	m.width, m.height = 120, 40
	before := dashKeyStyle.GetForeground()
	mi, _ := m.Update(tea.KeyPressMsg{Text: "T", Code: 'T'})
	m = mi.(dashboardModel)
	if !strings.Contains(m.View().Content, "Colour theme") {
		t.Fatal("T opens the theme popup")
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = mi.(dashboardModel)
	if theme.Current() == orig || dashKeyStyle.GetForeground() == before {
		t.Error("moving in the popup previews the theme live and rebuilds the cached styles")
	}
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := mi.(dashboardModel).actionMsg; got != "Theme: "+theme.Current() {
		t.Errorf("enter confirms the theme, got %q", got)
	}
}

func TestTenthAndPostctlCardsGetJumpKeys(t *testing.T) {
	cards := filterCards(allDashboardCards, nil) // all 12 cards
	if cards[9].key != "0" {
		t.Errorf("10th card key = %q, want 0", cards[9].key)
	}
	for _, c := range cards[9:] {
		if c.tool == "postctl" && c.key != "p" {
			t.Errorf("postctl card key = %q, want p", c.key)
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
