package cmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestTruncateRespectsDisplayWidthNotRuneCount(t *testing.T) {
	// Regression test: a wide character (emoji) inflates display width
	// without adding many runes. The old truncate() checked rune count as
	// a second bailout after the width check failed, so a short-in-runes
	// but wide-on-screen string slipped through untruncated — found via a
	// note title with an emoji making its dashboard card overflow its
	// fixed width and wrap onto an extra line, misaligning that row.
	s := "👶 Baby-Checkliste — Geburt ca. 28.12.2026 (Graz)"
	max := 20
	out := truncate(s, max)
	if w := lipgloss.Width(out); w > max {
		t.Errorf("truncate(%q, %d) = %q, want display width <= %d, got %d", s, max, out, max, w)
	}
}

func TestDashboardViewRenders(t *testing.T) {
	m := newDashboardModel()
	out := m.View()
	if !strings.Contains(out, "Tasks") || !strings.Contains(out, "Mail") {
		t.Errorf("expected all rows in view, got:\n%s", out)
	}
}

func TestDashboardCursorMovement(t *testing.T) {
	// Cards form a 2-column grid, so j/down must move a full row (+cardCols)
	// and l/right must move one column (+1) — not the other way around.
	m := newDashboardModel()
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = mi.(dashboardModel)
	if m.cursor != cardCols {
		t.Errorf("expected cursor %d after j (down one row), got %d", cardCols, m.cursor)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = mi.(dashboardModel)
	if m.cursor != 0 {
		t.Errorf("expected cursor 0 after k (up one row), got %d", m.cursor)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = mi.(dashboardModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor 1 after l (right one column), got %d", m.cursor)
	}
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = mi.(dashboardModel)
	if m.cursor != 0 {
		t.Errorf("expected cursor 0 after h (left one column), got %d", m.cursor)
	}
}

func TestDashboardCursorMovementStaysInBounds(t *testing.T) {
	// l/right at the last column of a row must not wrap into the next row.
	m := newDashboardModel()
	m.cursor = 1 // last column of row 0
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = mi.(dashboardModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor to stay at 1 (right edge of row), got %d", m.cursor)
	}

	// h/left at the first column of a row must not wrap into the previous row.
	m.cursor = 2 // first column of row 1
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = mi.(dashboardModel)
	if m.cursor != 2 {
		t.Errorf("expected cursor to stay at 2 (left edge of row), got %d", m.cursor)
	}
}

func TestDashboardQuit(t *testing.T) {
	m := newDashboardModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected a command (tea.Quit) when pressing q")
	}
}

func TestDashboardDigitJumpsAndMovesCursor(t *testing.T) {
	m := newDashboardModel()
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mi.(dashboardModel)
	if m.cursor != 4 {
		t.Errorf("expected cursor on row 4 (Budget) after pressing 5, got %d", m.cursor)
	}
	if cmd == nil {
		t.Error("expected a launch command when pressing a mapped digit")
	}
}

func TestDashboardUnmappedKeyIsNoop(t *testing.T) {
	m := newDashboardModel()
	mi, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m2 := mi.(dashboardModel)
	if m2.cursor != m.cursor {
		t.Error("expected cursor unchanged for an unmapped key")
	}
	if cmd != nil {
		t.Error("expected no command for an unmapped key")
	}
}
