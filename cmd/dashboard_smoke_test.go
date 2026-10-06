package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	out := ansi.Strip(m.viewContent()) // v2 styles always emit ANSI
	if !strings.Contains(out, "Tasks") || !strings.Contains(out, "Mail") {
		t.Errorf("expected all rows in view, got:\n%s", out)
	}
}

func TestDashboardCursorMovement(t *testing.T) {
	// Cards form a 2-column grid, so j/down must move a full row (+cardCols)
	// and l/right must move one column (+1) — not the other way around.
	m := newDashboardModel()
	mi, _ := m.Update(tea.KeyPressMsg{Text: "j", Code: []rune("j")[0]})
	m = mi.(dashboardModel)
	if m.cursor != cardCols {
		t.Errorf("expected cursor %d after j (down one row), got %d", cardCols, m.cursor)
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "k", Code: []rune("k")[0]})
	m = mi.(dashboardModel)
	if m.cursor != 0 {
		t.Errorf("expected cursor 0 after k (up one row), got %d", m.cursor)
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "l", Code: []rune("l")[0]})
	m = mi.(dashboardModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor 1 after l (right one column), got %d", m.cursor)
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: []rune("h")[0]})
	m = mi.(dashboardModel)
	if m.cursor != 0 {
		t.Errorf("expected cursor 0 after h (left one column), got %d", m.cursor)
	}
}

func TestDashboardCursorMovementStaysInBounds(t *testing.T) {
	// l/right at the last column of a row must not wrap into the next row.
	m := newDashboardModel()
	m.cursor = 1 // last column of row 0
	mi, _ := m.Update(tea.KeyPressMsg{Text: "l", Code: []rune("l")[0]})
	m = mi.(dashboardModel)
	if m.cursor != 1 {
		t.Errorf("expected cursor to stay at 1 (right edge of row), got %d", m.cursor)
	}

	// h/left at the first column of a row must not wrap into the previous row.
	m.cursor = 2 // first column of row 1
	mi, _ = m.Update(tea.KeyPressMsg{Text: "h", Code: []rune("h")[0]})
	m = mi.(dashboardModel)
	if m.cursor != 2 {
		t.Errorf("expected cursor to stay at 2 (left edge of row), got %d", m.cursor)
	}
}

func TestDashboardQuit(t *testing.T) {
	m := newDashboardModel()
	_, cmd := m.Update(tea.KeyPressMsg{Text: "q", Code: []rune("q")[0]})
	if cmd == nil {
		t.Fatal("expected a command (tea.Quit) when pressing q")
	}
}

func TestDashboardDigitJumpsAndMovesCursor(t *testing.T) {
	m := newDashboardModel()
	mi, cmd := m.Update(tea.KeyPressMsg{Text: "5", Code: []rune("5")[0]})
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
	mi, cmd := m.Update(tea.KeyPressMsg{Text: "z", Code: []rune("z")[0]})
	m2 := mi.(dashboardModel)
	if m2.cursor != m.cursor {
		t.Error("expected cursor unchanged for an unmapped key")
	}
	if cmd != nil {
		t.Error("expected no command for an unmapped key")
	}
}

func TestDashboardStartsWithoutBlockingAndShowsLoading(t *testing.T) {
	m := newDashboardModel()
	if !m.loading {
		t.Error("a fresh dashboard must be loading; the constructor must not shell out synchronously")
	}
	if out := ansi.Strip(m.viewContent()); !strings.Contains(out, "loading…") {
		t.Errorf("cards without data should say loading…, got:\n%s", out)
	}
}

func TestDashboardCardsLoadedFillsValues(t *testing.T) {
	m := newDashboardModel()
	vals := make([]cardStatus, len(dashboardCards))
	vals[0] = cardStatus{text: "3 open\nnext: Steuer"}
	mi, _ := m.Update(cardsLoadedMsg{values: vals, at: time.Now()})
	m = mi.(dashboardModel)
	if m.loading || !m.loaded[0] {
		t.Fatalf("loading=%v loaded[0]=%v after cardsLoadedMsg", m.loading, m.loaded[0])
	}
	out := ansi.Strip(m.viewContent())
	if !strings.Contains(out, "3 open") || strings.Contains(out, "loading…") {
		t.Errorf("loaded values not rendered (or still loading):\n%s", out)
	}
}

func TestDashboardRefreshNotStartedTwice(t *testing.T) {
	m := newDashboardModel() // loading already
	if cmd := m.startRefresh(); cmd != nil {
		t.Error("startRefresh while a refresh is running must be a no-op")
	}
	m.loading = false
	if cmd := m.startRefresh(); cmd == nil || !m.loading {
		t.Error("startRefresh when idle must start one")
	}
}

func TestDashboardFocusReloadsOnlyWhenStale(t *testing.T) {
	m := newDashboardModel()
	m.loading = false
	m.lastRefresh = time.Now()
	m.now = time.Now()
	if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
		t.Error("focus right after a refresh must not reload")
	}
	m.lastRefresh = time.Now().Add(-time.Minute)
	m.now = time.Now()
	if _, cmd := m.Update(tea.FocusMsg{}); cmd == nil {
		t.Error("focus with stale data must reload")
	}
}

func TestDashboardClickSelectsAndDoubleClickLaunches(t *testing.T) {
	m := newDashboardModel()
	m.width, m.height = 100, 40
	w := m.cardWidth()
	// second column, first row: inside the card at index 1
	x, y := len(rowIndent)+w+len(cardGap)+2, 5
	if got := m.cardAt(x, y); got != 1 {
		t.Fatalf("cardAt(%d,%d) = %d, want 1", x, y, got)
	}
	if got := m.cardAt(0, 0); got != -1 {
		t.Errorf("header area hit a card: %d", got)
	}
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}
	mi, cmd := m.Update(click)
	m = mi.(dashboardModel)
	if m.cursor != 1 || cmd != nil {
		t.Fatalf("first click: cursor=%d cmd=%v, want select only", m.cursor, cmd != nil)
	}
	if _, cmd = m.Update(click); cmd == nil {
		t.Error("second quick click on the same card must launch it")
	}
}

func TestDashboardHoverTracksMouse(t *testing.T) {
	m := newDashboardModel()
	m.width = 100
	x, y := len(rowIndent)+2, 5
	mi, _ := m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if got := mi.(dashboardModel).hover; got != 0 {
		t.Errorf("hover = %d, want 0", got)
	}
	mi, _ = mi.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	if got := mi.(dashboardModel).hover; got != -1 {
		t.Errorf("hover off the grid = %d, want -1", got)
	}
}

func TestDashboardHelpPopup(t *testing.T) {
	m := newDashboardModel()
	m.width, m.height = 100, 40
	mi, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = mi.(dashboardModel)
	if !m.showHelp {
		t.Fatal("? must open help")
	}
	if out := ansi.Strip(m.viewContent()); !strings.Contains(out, "sync all tools") {
		t.Errorf("help popup not rendered:\n%s", out)
	}
	mi, cmd := m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	m = mi.(dashboardModel)
	if m.showHelp || cmd != nil {
		t.Errorf("any key closes help without acting: showHelp=%v cmd=%v", m.showHelp, cmd != nil)
	}
}

func TestDashboardSearchFlow(t *testing.T) {
	m := newDashboardModel()
	m.width, m.height = 100, 30
	mi, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = mi.(dashboardModel)
	if !m.showSearch || !m.searchTyping {
		t.Fatalf("/ must open search in typing mode: show=%v typing=%v", m.showSearch, m.searchTyping)
	}
	for _, r := range "steuer" {
		mi, _ = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = mi.(dashboardModel)
	}
	if got := m.searchInput.Value(); got != "steuer" {
		t.Fatalf("query = %q", got)
	}
	// space must be typeable (v2 reports it as "space"; it still has to reach the field)
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	m = mi.(dashboardModel)
	if got := m.searchInput.Value(); got != "steuer " {
		t.Errorf("space not typed into the query: %q", got)
	}
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)
	if !m.searchBusy || cmd == nil {
		t.Fatalf("enter must start a search: busy=%v cmd=%v", m.searchBusy, cmd != nil)
	}
	if out := ansi.Strip(m.viewContent()); !strings.Contains(out, "searching…") {
		t.Errorf("busy view:\n%s", out)
	}

	hits := []SearchHit{{Tool: "taskctl", Icon: "✓", Title: "Steuer abgeben"}, {Tool: "notectl", Icon: "📝", Title: "Steuer-Notiz"}}
	mi, _ = m.Update(searchResultMsg{hits: hits})
	m = mi.(dashboardModel)
	if m.searchBusy || m.searchTyping || len(m.searchHits) != 2 {
		t.Fatalf("after results: busy=%v typing=%v hits=%d (want browsing)", m.searchBusy, m.searchTyping, len(m.searchHits))
	}
	if out := ansi.Strip(m.viewContent()); !strings.Contains(out, "Steuer abgeben") || !strings.Contains(out, "▸") {
		t.Errorf("results view:\n%s", out)
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = mi.(dashboardModel)
	if m.searchCursor != 1 {
		t.Errorf("cursor = %d, want 1", m.searchCursor)
	}
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(dashboardModel)
	if m.showSearch || cmd == nil {
		t.Errorf("enter on a hit launches its tool and closes search: show=%v cmd=%v", m.showSearch, cmd != nil)
	}
}

func TestDashboardSearchEscAndEmptyResult(t *testing.T) {
	m := newDashboardModel()
	mi, _ := m.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	m = mi.(dashboardModel)
	mi, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // empty query: nothing to run
	m = mi.(dashboardModel)
	if m.searchBusy || cmd != nil {
		t.Error("empty query must not start a search")
	}
	mi, _ = m.Update(searchResultMsg{})
	m = mi.(dashboardModel)
	if !m.searchTyping || !strings.Contains(ansi.Strip(m.viewContent()), "Nothing found.") {
		t.Errorf("no hits: typing=%v view:\n%s", m.searchTyping, ansi.Strip(m.viewContent()))
	}
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if mi.(dashboardModel).showSearch {
		t.Error("esc closes search")
	}
}

func TestFilterCardsOrderUnknownAndRenumber(t *testing.T) {
	got := filterCards(allDashboardCards, []string{"Habits", "nonsense", "tasks", "habits"})
	if len(got) != 2 || got[0].label != "Habits" || got[1].label != "Tasks" {
		t.Fatalf("order/dedup/unknown: %+v", got)
	}
	if got[0].key != "1" || got[1].key != "2" {
		t.Errorf("keys must be renumbered to match the screen: %q %q", got[0].key, got[1].key)
	}
	if all := filterCards(allDashboardCards, nil); len(all) != len(allDashboardCards) {
		t.Errorf("no list = all cards, got %d", len(all))
	}
	if all := filterCards(allDashboardCards, []string{"nope"}); len(all) != len(allDashboardCards) {
		t.Error("only unknown ids must fall back to all cards, not an empty dashboard")
	}
}

func TestDashboardWithThreeCards(t *testing.T) {
	orig := dashboardCards
	dashboardCards = filterCards(allDashboardCards, []string{"tasks", "habits", "notes"})
	t.Cleanup(func() { dashboardCards = orig })

	m := newDashboardModel()
	m.width, m.height = 100, 30
	mi, _ := m.Update(cardsLoadedMsg{values: make([]cardStatus, 3), at: time.Now()})
	m = mi.(dashboardModel)
	out := ansi.Strip(m.viewContent())
	if !strings.Contains(out, "Habits") || strings.Contains(out, "Mail") || strings.Contains(out, "Budget") {
		t.Errorf("only the chosen cards should render:\n%s", out)
	}
	// 3 cards in 2 columns: the last row has one card; cursor must not run past it
	m.cursor = 2
	mi, _ = m.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	if got := mi.(dashboardModel).cursor; got != 2 {
		t.Errorf("cursor ran off the last card: %d", got)
	}
	// number keys address the visible cards
	mi, cmd := m.Update(tea.KeyPressMsg{Text: "2", Code: '2'})
	if mi.(dashboardModel).cursor != 1 || cmd == nil {
		t.Error("2 must jump to the second visible card (habits)")
	}
}

func loadedDashboard(t *testing.T) dashboardModel {
	t.Helper()
	m := newDashboardModel()
	m.width, m.height = 100, 30
	vals := make([]cardStatus, len(dashboardCards))
	vals[0] = cardStatus{text: "2 due today\nSteuer", items: []string{"Steuer  · due Oct 6", "Rechnung"}, spark: []float64{0, 1, 0, 2, 0, 0, 3}}
	vals[1] = cardStatus{text: "no events today"} // no items
	mi, _ := m.Update(cardsLoadedMsg{values: vals, at: time.Now()})
	return mi.(dashboardModel)
}

func TestDashboardSparklineShownOnCard(t *testing.T) {
	m := loadedDashboard(t)
	out := ansi.Strip(m.viewContent())
	if !strings.Contains(out, sparkline([]float64{0, 1, 0, 2, 0, 0, 3})) {
		t.Errorf("sparkline missing from the tasks card:\n%s", out)
	}
	// a sparkline must not change the card's height (same card, with and without)
	with := lipgloss.Height(m.renderCard(0))
	m.values[0].spark = nil
	if without := lipgloss.Height(m.renderCard(0)); with != without {
		t.Errorf("sparkline changed the card height: %d vs %d", with, without)
	}
}

// Regression: lipgloss v2's Width() includes border and padding, so sizing the
// card body to w-2 wrapped the [n] key badge onto its own row.
func TestDashboardCardHeaderStaysOnOneLine(t *testing.T) {
	m := loadedDashboard(t)
	for _, width := range []int{60, 80, 100, 140} {
		m.width = width
		lines := strings.Split(ansi.Strip(m.renderCard(0)), "\n")
		if !strings.Contains(lines[1], "Tasks") || !strings.Contains(lines[1], "[1]") {
			t.Errorf("width %d: label and key badge must share the first body line, got %q", width, lines[1])
		}
		for _, l := range lines {
			if lipgloss.Width(l) != m.cardWidth() {
				t.Errorf("width %d: line %q is %d cells, card is %d", width, l, lipgloss.Width(l), m.cardWidth())
			}
		}
	}
}

func TestDashboardDrillDownPopup(t *testing.T) {
	m := loadedDashboard(t)
	mi, cmd := m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	m = mi.(dashboardModel)
	if !m.showDrill || cmd != nil {
		t.Fatalf("d opens the peek popup without launching: show=%v cmd=%v", m.showDrill, cmd != nil)
	}
	out := ansi.Strip(m.viewContent())
	for _, want := range []string{"Tasks", "2 due today", "Steuer  · due Oct 6", "Rechnung", "enter open taskctl"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup missing %q:\n%s", want, out)
		}
	}
	// esc (or any other key) closes without launching
	mi, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if mi.(dashboardModel).showDrill || cmd != nil {
		t.Error("esc must close the popup and do nothing else")
	}
	// enter closes AND launches the tool
	mi, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	mi, cmd = mi.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mi.(dashboardModel).showDrill || cmd == nil {
		t.Errorf("enter closes and launches: show=%v cmd=%v", mi.(dashboardModel).showDrill, cmd != nil)
	}
}

func TestDashboardDrillDownEdgeCases(t *testing.T) {
	m := loadedDashboard(t)
	// a card without items says so instead of showing an empty box
	m.cursor = 1
	mi, _ := m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if out := ansi.Strip(mi.(dashboardModel).viewContent()); !strings.Contains(out, "no item list for this card") {
		t.Errorf("no-items card:\n%s", out)
	}
	// not loaded yet → loading…
	fresh := newDashboardModel()
	fresh.width, fresh.height = 100, 30
	mi, _ = fresh.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if out := ansi.Strip(mi.(dashboardModel).viewContent()); !strings.Contains(out, "loading…") {
		t.Errorf("unloaded card:\n%s", out)
	}
	// long lists are capped
	many := make([]string, 30)
	for i := range many {
		many[i] = "item " + strings.Repeat("x", i%3)
	}
	m = loadedDashboard(t)
	m.values[0].items = many
	m.cursor = 0
	mi, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if out := ansi.Strip(mi.(dashboardModel).viewContent()); !strings.Contains(out, "…and 18 more") {
		t.Errorf("cap at %d items missing:\n%s", drillMaxItems, out)
	}
	// space peeks too; the agenda view has no peek
	m = loadedDashboard(t)
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !mi.(dashboardModel).showDrill {
		t.Error("space must open the peek popup")
	}
	m.showAgenda = true
	mi, _ = m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if mi.(dashboardModel).showDrill {
		t.Error("no peek popup over the agenda view")
	}
}

func TestLoadCardIDsFromSuiteConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := loadCardIDs(); got != nil {
		t.Errorf("no file → nil (default: all cards), got %v", got)
	}
	dir := home + "/.config/missionctl"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/dashboard.yaml", []byte("cards: [tasks, habits]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadCardIDs()
	if len(got) != 2 || got[0] != "tasks" || got[1] != "habits" {
		t.Errorf("loadCardIDs = %v", got)
	}
	if want := dir + "/notified.json"; notifyStatePath() != want {
		t.Errorf("notify state lives next to theme.yaml: %s, want %s", notifyStatePath(), want)
	}
}
