package cmd

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"
)

type dashboardCard struct {
	key    string
	icon   string
	label  string
	tool   string // binary to launch on keypress; "" = no jump target
	color  color.Color
	value  func(now time.Time) cardStatus
	action func() (string, error) // "x" quick action; nil = none for this card
}

var dashboardCards = []dashboardCard{
	{"1", "✓", "Tasks", "taskctl", lipgloss.Color("39"), func(_ time.Time) cardStatus { return taskStatus() }, quickCompleteTask},
	{"2", "📅", "Calendar", "calctl", lipgloss.Color("42"), calStatus, nil},
	{"3", "⏱", "Timer", "timectl", lipgloss.Color("221"), timerStatus, quickStopTimer},
	{"4", "📔", "Diary", "diaryctl", lipgloss.Color("212"), func(_ time.Time) cardStatus { return diaryStatus() }, nil},
	{"5", "💰", "Budget", "budgetctl", lipgloss.Color("208"), budgetStatus, nil},
	{"6", "🔥", "Habits", "habctl", lipgloss.Color("203"), habitStatus, quickCheckHabit},
	{"7", "📝", "Notes", "notectl", lipgloss.Color("135"), noteStatus, nil},
	{"8", "✉", "Mail", "mailctl", lipgloss.Color("33"), func(_ time.Time) cardStatus { return mailStatus() }, nil},
}

// quickCompleteTask/quickCheckHabit/quickStopTimer re-fetch the same --json
// data their card's status function already showed, act on the first
// actionable item, and shell out to the tool's own write command — no new
// data path, just acting on what's already on screen instead of requiring
// a trip into the full tool for a one-line action.

func quickCompleteTask() (string, error) {
	var resp struct {
		Data []struct {
			Title string `json:"title"`
			List  string `json:"list"`
		} `json:"data"`
	}
	if !runToolJSON("taskctl", []string{"today", "--json"}, &resp) || len(resp.Data) == 0 {
		return "", fmt.Errorf("no due task to complete")
	}
	t := resp.Data[0]
	args := []string{"done", t.Title}
	if t.List != "" {
		args = append(args, "--list", t.List)
	}
	if out, err := exec.Command("taskctl", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return "completed: " + t.Title, nil
}

func quickCheckHabit() (string, error) {
	var resp struct {
		Data []struct {
			Name         string `json:"name"`
			CheckedToday bool   `json:"checked_today"`
		} `json:"data"`
	}
	if !runToolJSON("habctl", []string{"today", "--json"}, &resp) {
		return "", fmt.Errorf("habctl not available")
	}
	for _, h := range resp.Data {
		if h.CheckedToday {
			continue
		}
		if out, err := exec.Command("habctl", "check", h.Name).CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return "checked: " + h.Name, nil
	}
	return "", fmt.Errorf("all habits already done today")
}

func quickStopTimer() (string, error) {
	var resp struct {
		Entries []struct {
			Running bool `json:"running"`
		} `json:"entries"`
	}
	if !runToolJSON("timectl", []string{"today", "--json"}, &resp) {
		return "", fmt.Errorf("timectl not available")
	}
	running := false
	for _, e := range resp.Entries {
		if e.Running {
			running = true
		}
	}
	if !running {
		return "", fmt.Errorf("no timer running")
	}
	if out, err := exec.Command("timectl", "stop").CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return "timer stopped", nil
}

// syncableTools are the ones that pull from an external source (Apple
// Reminders/Calendar/Mail, an Obsidian vault) and so actually have a
// `sync` subcommand. The other 4 (timectl, budgetctl, habctl, diaryctl)
// are locally-authored — their database already is the source of truth,
// there's nothing external to pull from — so they're not in this list.
var syncableTools = []string{"taskctl", "calctl", "notectl", "mailctl"}

// syncStepMsg reports one syncableTools entry finishing. Sync-all used to
// run every tool in one blocking call and just show "running…" the whole
// time — mailctl alone routinely takes 45-90s (many individual AppleScript
// round-trips per message, and slower still with no stdio attached, which
// is exactly how exec.Command runs it here), so a user checking back after
// 20-30s reasonably concluded it had silently failed. Now each tool's sync
// runs as its own step so the status line can name which one is in flight.
type syncStepMsg struct {
	idx int // index into syncableTools that just finished
	err error
}

func syncStepCmd(idx int) tea.Cmd {
	return func() tea.Msg {
		err := exec.Command(syncableTools[idx], "sync").Run()
		return syncStepMsg{idx: idx, err: err}
	}
}

func syncStepLabel(idx int) string {
	return fmt.Sprintf("syncing %s… (%d/%d)", syncableTools[idx], idx+1, len(syncableTools))
}

// Icons are plain Unicode emoji (not Nerd Font glyphs) so they render
// correctly in any modern terminal font, not just ones with icon patches.

type tickMsg time.Time

// urgencyLevel lets a card override its normally-fixed border color to
// flag something that actually needs attention (overdue tasks, a budget
// goal blown) instead of every card always looking equally calm.
type urgencyLevel int

const (
	urgencyNormal urgencyLevel = iota
	urgencyWarn
	urgencyCritical
)

// cardStatus is what a card's value function returns: the same
// "summary\ndetail" text as before, plus how urgent it is. Bundled
// together (rather than a second parallel function) so computing urgency
// never requires re-fetching the same --json data status text already
// fetched once per refresh.
type cardStatus struct {
	text    string
	urgency urgencyLevel
}

type dashboardModel struct {
	cursor      int
	err         error
	width       int
	height      int
	values      [8]cardStatus
	lastRefresh time.Time
	loading     bool    // a refresh is in flight; the UI stays responsive meanwhile
	loaded      [8]bool // per card: has a value arrived yet (else it shows "loading…")
	hover       int     // card under the mouse, -1 for none
	lastClick   int     // card of the previous left click, for double-click → launch
	lastClickAt time.Time
	showHelp    bool // "?" toggles the key reference popup

	showSearch   bool // "/" opens the cross-tool search (see search.go)
	searchInput  textinput.Model
	searchTyping bool // true while the query field has focus, false while browsing results
	searchBusy   bool
	searchRan    bool
	searchHits   []SearchHit
	searchCursor int
	now          time.Time
	actionMsg    string // result of the last "x" quick action, cleared on next action/refresh
	actionBusy   bool
	syncFailed   []string // syncableTools entries whose step errored, accumulated across a sync-all run

	showAgenda   bool // "a" toggles between the card grid and the agenda view
	agendaLoad   bool
	agendaAllDay []agendaItem
	agendaTimed  []agendaItem

	showSettings    bool // "L" toggles the license settings screen (see settings.go)
	settingsInput   textinput.Model
	settingsBusy    bool
	settingsResults []toolResult
	settingsMsg     string
}

func newDashboardModel() dashboardModel {
	ti := textinput.New()
	ti.Placeholder = "paste your Bundle key…"
	ti.CharLimit = 200
	ti.SetWidth(50)

	si := textinput.New()
	si.Placeholder = "search tasks, events, notes, mail, budget, diary, time, habits…"
	si.CharLimit = 120
	si.SetWidth(60)

	return dashboardModel{now: time.Now(), width: 80, settingsInput: ti, searchInput: si, hover: -1, lastClick: -1, loading: true}
}

// cardsLoadedMsg carries every card's status, fetched in parallel off the UI
// goroutine: each value() shells out to a tool's --json, and doing eight of
// those one after another inside Update froze the dashboard at startup and
// every 30 seconds.
type cardsLoadedMsg struct {
	values [8]cardStatus
	at     time.Time
}

func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		var vals [8]cardStatus
		var wg sync.WaitGroup
		for i, c := range dashboardCards {
			wg.Add(1)
			go func() {
				defer wg.Done()
				vals[i] = c.value(now)
			}()
		}
		wg.Wait()
		return cardsLoadedMsg{values: vals, at: now}
	}
}

// startRefresh kicks off a background refresh unless one is already running.
func (m *dashboardModel) startRefresh() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	return refreshCmd()
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(tickEvery(time.Second), refreshCmd())
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.settingsMsgUpdate(msg); handled {
		return next, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.MouseMotionMsg:
		m.hover = -1
		if !m.showAgenda && !m.showSettings && !m.showHelp {
			m.hover = m.cardAt(msg.X, msg.Y)
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.showAgenda || m.showSettings || m.showHelp {
			return m, nil
		}
		i := m.cardAt(msg.X, msg.Y)
		if i < 0 {
			return m, nil
		}
		now := time.Now()
		if i == m.lastClick && now.Sub(m.lastClickAt) < 400*time.Millisecond {
			m.lastClick = -1 // consumed, so a third click starts fresh
			return m, m.launch(dashboardCards[i].tool)
		}
		m.cursor, m.lastClick, m.lastClickAt = i, i, now
		return m, nil

	case tea.MouseMsg: // wheel etc. — swallowed on purpose
		// The card grid always fits on screen — there's nothing to scroll.
		// Without mouse capture enabled, a trackpad/wheel scroll gets
		// translated by the terminal into arrow-key escapes instead, which
		// used to jump the card cursor around unintentionally.
		return m, nil

	case tea.FocusMsg:
		// Coming back from another window (or a tool we launched): the data
		// is probably stale, so reload — but not on every focus flicker.
		if m.now.Sub(m.lastRefresh) > 5*time.Second {
			return m, m.startRefresh()
		}
		return m, nil

	case searchResultMsg:
		m.searchBusy, m.searchRan = false, true
		m.searchHits, m.searchCursor = msg.hits, 0
		m.searchTyping = len(msg.hits) == 0 // nothing to browse → keep typing
		if m.searchTyping {
			m.searchInput.Focus()
		}
		return m, nil

	case cardsLoadedMsg:
		m.values = msg.values
		for i := range m.loaded {
			m.loaded[i] = true
		}
		m.loading = false
		m.lastRefresh = msg.at
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if m.now.Sub(m.lastRefresh) >= 30*time.Second {
			return m, tea.Batch(m.startRefresh(), tickEvery(time.Second))
		}
		return m, tickEvery(time.Second)

	case tea.KeyPressMsg:
		if m.showSettings {
			return m.updateSettings(msg)
		}
		if m.showSearch {
			return m.updateSearch(msg)
		}
		if m.showHelp {
			// any key closes the popup; ctrl+c still quits
			m.showHelp = false
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "/":
			if m.showAgenda {
				return m, nil
			}
			m.showSearch, m.searchTyping = true, true
			m.searchRan, m.searchHits, m.searchCursor = false, nil, 0
			m.searchInput.SetValue("")
			m.searchInput.Focus()
			return m, nil
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if !m.showAgenda && m.cursor+cardCols < len(dashboardCards) {
				m.cursor += cardCols
			}
			return m, nil
		case "k", "up":
			if !m.showAgenda && m.cursor-cardCols >= 0 {
				m.cursor -= cardCols
			}
			return m, nil
		case "h", "left":
			if !m.showAgenda && m.cursor%cardCols != 0 {
				m.cursor--
			}
			return m, nil
		case "l", "right":
			if !m.showAgenda && m.cursor%cardCols != cardCols-1 && m.cursor < len(dashboardCards)-1 {
				m.cursor++
			}
			return m, nil
		case "r":
			if m.showAgenda {
				m.agendaLoad = true
				return m, loadAgendaCmd()
			}
			return m, m.startRefresh()
		case "a":
			m.showAgenda = !m.showAgenda
			if m.showAgenda {
				m.agendaLoad = true
				return m, loadAgendaCmd()
			}
			return m, nil
		case "enter":
			if m.showAgenda {
				return m, nil
			}
			return m, m.launch(dashboardCards[m.cursor].tool)
		case "x":
			if m.showAgenda {
				return m, nil
			}
			card := dashboardCards[m.cursor]
			if card.action == nil || m.actionBusy {
				return m, nil
			}
			m.actionBusy = true
			m.actionMsg = ""
			return m, runQuickAction(card.action)
		case "s":
			if m.actionBusy {
				return m, nil
			}
			m.actionBusy = true
			m.syncFailed = nil
			m.actionMsg = syncStepLabel(0)
			return m, syncStepCmd(0)
		case "L":
			if m.showAgenda {
				return m, nil
			}
			m.showSettings = true
			m.settingsMsg = ""
			m.settingsInput.SetValue("")
			m.settingsInput.Focus()
			m.settingsBusy = true
			return m, loadSettingsStatusCmd()
		}
		if !m.showAgenda {
			for i, c := range dashboardCards {
				if msg.String() == c.key {
					m.cursor = i
					return m, m.launch(c.tool)
				}
			}
		}

	case launchErrMsg:
		m.err = msg.err
		return m, m.startRefresh()

	case quickActionMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.actionMsg = "✗ " + msg.err.Error()
		} else {
			m.actionMsg = "✓ " + msg.result
			return m, m.startRefresh()
		}
		return m, nil

	case syncStepMsg:
		if msg.err != nil {
			m.syncFailed = append(m.syncFailed, syncableTools[msg.idx])
		}
		next := msg.idx + 1
		if next < len(syncableTools) {
			m.actionMsg = syncStepLabel(next)
			return m, syncStepCmd(next)
		}
		m.actionBusy = false
		if len(m.syncFailed) > 0 {
			m.actionMsg = "✗ failed: " + strings.Join(m.syncFailed, ", ")
		} else {
			m.actionMsg = "✓ synced " + strings.Join(syncableTools, ", ")
		}
		return m, m.startRefresh()

	case agendaLoadedMsg:
		m.agendaLoad = false
		m.agendaAllDay = msg.allDay
		m.agendaTimed = msg.timed
		return m, nil
	}
	return m, nil
}

// agendaLoadedMsg carries the same calendar/task/timer merge that the
// standalone `agenda` command prints, computed off the UI goroutine (it
// shells out to three tools) so toggling the view never freezes input.
type agendaLoadedMsg struct{ allDay, timed []agendaItem }

func loadAgendaCmd() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		allDay, timed := splitAgendaItems(buildAgendaItems(now))
		return agendaLoadedMsg{allDay: allDay, timed: timed}
	}
}

type quickActionMsg struct {
	result string
	err    error
}

func runQuickAction(action func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		result, err := action()
		return quickActionMsg{result: result, err: err}
	}
}

type launchErrMsg struct{ err error }

// launch suspends the dashboard's terminal control, runs the target tool's
// TUI in the foreground, and resumes (and refreshes) the dashboard once it
// exits.
func (m dashboardModel) launch(tool string) tea.Cmd {
	if tool == "" {
		return nil
	}
	c := exec.Command(tool)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return launchErrMsg{fmt.Errorf("%s: %w", tool, err)}
		}
		return launchErrMsg{nil}
	})
}

// Dashboard status/chrome colors used to be hardcoded lipgloss.Color values
// tuned only for a dark terminal background (dashFg was plain white "255"
// with no background behind it in dashKeyStyle/summaryStyle — invisible on
// a light terminal theme). Switched to missionctl-core/theme's
// AdaptiveColor palette, same one the other seven tools in the suite
// already share, so the dashboard follows the terminal's light/dark mode
// like everything else does.
// cliOut is where CLI (non-TUI) commands print: lipgloss v2 styles always emit
// ANSI, so this strips/downsamples it for pipes and NO_COLOR like v1 did.
var cliOut = colorprofile.NewWriter(os.Stdout, os.Environ())

var (
	dashMuted         = theme.MutedV2
	dashSubtle        = theme.SubtleV2
	dashErrColor      = theme.RedV2
	dashWarnColor     = theme.AmberV2
	dashCriticalColor = theme.RedV2

	dashTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.OnAccentV2).
			Background(lipgloss.Color("57")).
			Padding(0, 2)

	dashTaglineStyle = lipgloss.NewStyle().Foreground(dashSubtle).Italic(true)
	dashRuleStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashClockStyle   = lipgloss.NewStyle().Foreground(dashMuted)
	dashFootStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashKeyStyle     = lipgloss.NewStyle().Foreground(theme.AmberV2).Bold(true)
	dashErrStyle     = lipgloss.NewStyle().Foreground(dashErrColor).Bold(true)
	dashOKStyle      = lipgloss.NewStyle().Foreground(theme.GreenV2).Bold(true)
	dashMutedStyle   = lipgloss.NewStyle().Foreground(dashMuted)

	// checkMark/crossMark/dashMark/nameStyle: the ✓/✗/– status marks and
	// name-column width shared by doctor/install/update/license/settings'
	// per-tool status lines — previously redeclared identically in each of
	// those files.
	checkMark = dashOKStyle.Render("✓")
	crossMark = dashErrStyle.Render("✗")
	dashMark  = lipgloss.NewStyle().Foreground(dashSubtle).Render("–")
	nameStyle = lipgloss.NewStyle().Width(14)
)

const (
	cardCols  = 2
	rowIndent = "  "
	cardGap   = "   "
)

func (m dashboardModel) cardWidth() int {
	// two columns with a visible gap between them, plus the row indent
	w := (m.width - len(rowIndent) - len(cardGap)) / cardCols
	if w < 24 {
		w = 24
	}
	if w > 44 {
		w = 44
	}
	return w
}

// cardAt maps a terminal cell to the card drawn there (-1 for none), using the
// same layout viewContent produces: a 4-line header block, then rows of
// cardCols cards whose height is that of the tallest card in the row.
func (m dashboardModel) cardAt(x, y int) int {
	const gridTop = 4 // blank, banner, rule, blank
	w := m.cardWidth()
	top := gridTop
	for row := 0; row < len(dashboardCards); row += cardCols {
		h := 0
		for col := 0; col < cardCols && row+col < len(dashboardCards); col++ {
			h = max(h, lipgloss.Height(m.renderCard(row+col)))
		}
		if y >= top && y < top+h {
			for col := 0; col < cardCols && row+col < len(dashboardCards); col++ {
				x0 := len(rowIndent) + col*(w+len(cardGap))
				if x >= x0 && x < x0+w {
					return row + col
				}
			}
			return -1
		}
		top += h
	}
	return -1
}

func (m dashboardModel) renderCard(i int) string {
	c := dashboardCards[i]
	w := m.cardWidth()
	selected := i == m.cursor

	var cardColor color.Color = c.color
	switch m.values[i].urgency {
	case urgencyCritical:
		cardColor = dashCriticalColor
	case urgencyWarn:
		cardColor = dashWarnColor
	}

	border := lipgloss.RoundedBorder()
	borderColor := cardColor
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(cardColor)
	if selected {
		border = lipgloss.ThickBorder()
		titleStyle = titleStyle.Underline(true)
	} else if i == m.hover {
		border = lipgloss.DoubleBorder()
	}

	keyBadge := lipgloss.NewStyle().Foreground(dashSubtle).Render("[" + c.key + "]")
	head := lipgloss.JoinHorizontal(lipgloss.Top,
		titleStyle.Render(c.icon+" "+c.label),
	)
	headLine := lipgloss.NewStyle().Width(w - 2).Render(head)
	// right-pad so the key badge lands flush right within the card interior
	pad := (w - 2) - lipgloss.Width(head) - lipgloss.Width(keyBadge)
	if pad < 1 {
		pad = 1
	}
	headLine = head + strings.Repeat(" ", pad) + keyBadge

	// Status values are "summary\ndetail" — a second line of specifics (the
	// actual overdue task's title, the next event, who's over budget) added
	// alongside the one-line counts already there. Styled dimmer than the
	// summary so the at-a-glance number stays the visual anchor.
	value := m.values[i]
	if !m.loaded[i] {
		value.text = "– loading…"
	}
	summaryLine, detailLine, _ := strings.Cut(value.text, "\n")

	// No Foreground set here on purpose — this is the primary card value
	// text and should inherit the terminal's own default foreground, which
	// is readable against that terminal's background by definition. The
	// old hardcoded white ("255") broke exactly this on light themes.
	summaryStyle := lipgloss.NewStyle().Width(w - 2)
	if summaryLine == "" || strings.HasPrefix(summaryLine, "–") {
		summaryStyle = summaryStyle.Foreground(dashSubtle)
	}
	valueBlock := summaryStyle.Render(truncate(summaryLine, w-2))
	// Always emit a second line (blank if there's no detail) so every card
	// is the same height — cards without one used to make lipgloss.JoinHorizontal
	// misalign that row's bottom borders against its taller row-mate.
	if detailLine != "" {
		detailStyle := lipgloss.NewStyle().Foreground(dashSubtle).Width(w - 2)
		valueBlock += "\n" + detailStyle.Render(truncate(detailLine, w-2))
	} else {
		valueBlock += "\n"
	}

	if age := syncAge(c.tool); age != "" {
		ageStyle := lipgloss.NewStyle().Foreground(dashSubtle).Width(w - 2)
		valueBlock += "\n" + ageStyle.Render(truncate("synced "+age, w-2))
	}

	body := headLine + "\n" + valueBlock

	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(w)

	return box.Render(body)
}

// truncate cuts s to at most max display columns, appending "…". Trims by
// display width (lipgloss.Width), not rune count — a wide character (an
// emoji, CJK) can make a string exceed max columns despite having fewer
// runes than max, which a rune-count-only check would miss and return the
// untruncated string, silently overflowing whatever fixed-width box it's
// meant to fit (found via a note title with an emoji making its dashboard
// card one line taller than its row-mates).
func truncate(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > max {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// renderHeader draws the full-width MISSIONCTL banner: badge + tagline on
// the left, live clock flush right, and a rule underneath. The tagline is
// dropped first if the terminal is too narrow to fit everything.
func (m dashboardModel) renderHeader() string {
	w := m.width
	if w < 40 {
		w = 40
	}
	contentW := w - len(rowIndent)

	badge := dashTitleStyle.Render("🛰  MISSIONCTL")
	tagline := dashTaglineStyle.Render("mission control for your terminal")
	clock := dashClockStyle.Render(m.now.Format("Mon Jan 02 · 15:04:05"))

	left := badge + "  " + tagline
	gap := contentW - lipgloss.Width(left) - lipgloss.Width(clock)
	if gap < 1 {
		left = badge // not enough room — drop the tagline first
		gap = contentW - lipgloss.Width(left) - lipgloss.Width(clock)
	}
	if gap < 1 {
		gap = 1
	}

	line := rowIndent + left + strings.Repeat(" ", gap) + clock
	rule := rowIndent + dashRuleStyle.Render(strings.Repeat("─", contentW))
	return line + "\n" + rule
}

// renderAgenda draws the merged calendar/task/timer timeline in place of the
// card grid, reusing the same data buildAgendaItems/splitAgendaItems produce
// for the standalone `agenda` command.
func (m dashboardModel) renderAgenda() string {
	if m.agendaLoad {
		return rowIndent + dashMutedStyle.Render("loading agenda…") + "\n"
	}

	var b strings.Builder
	timeStyle := lipgloss.NewStyle().Foreground(dashMuted).Width(6)

	if len(m.agendaAllDay) == 0 && len(m.agendaTimed) == 0 {
		b.WriteString(rowIndent + dashMutedStyle.Render("Nothing scheduled today.") + "\n")
		return b.String()
	}

	printItem := func(it agendaItem, timeLabel string) {
		iconStyle := lipgloss.NewStyle().Foreground(it.color)
		line := fmt.Sprintf("%s %s  %s", timeStyle.Render(timeLabel), iconStyle.Render(it.icon), it.text)
		b.WriteString(rowIndent + line + "\n")
	}
	for _, it := range m.agendaAllDay {
		printItem(it, "")
	}
	for _, it := range m.agendaTimed {
		printItem(it, it.when.Format("15:04"))
	}
	return b.String()
}

func (m dashboardModel) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's WithAltScreen/WithMouseCellMotion Program options are per-View fields in v2.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true // FocusMsg → reload stale data when the window regains focus
	return v
}

func (m dashboardModel) viewContent() string {
	if m.showSettings {
		return m.renderSettings()
	}
	if m.showSearch {
		return m.renderSearch()
	}
	if m.showHelp {
		return overlay.Center(m.gridContent(), m.renderHelpPopup(), m.width, m.height, 0)
	}
	return m.gridContent()
}

func (m dashboardModel) renderHelpPopup() string {
	h := keymap.Bare().
		Section("Navigate").
		Row("↑↓←→ / hjkl", "move between cards").
		Row("1-8", "open that tool").
		Row("enter", "open the selected tool").
		Row("click / double-click", "select / open a card").
		Section("Act").
		Row("x", "quick action on the card (complete task, stop timer, check habit)").
		Row("s", "sync all tools").
		Row("r", "reload now (auto every 30s and on window focus)").
		Row("a", "today's agenda").
		Row("/", "search across all tools").
		Row("L", "license settings").
		Section("Other").
		Row("?", "this help").
		Row("q / esc", "quit")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(dashSubtle).
		Padding(1, 2).Render(h.String())
}

func (m dashboardModel) gridContent() string {
	var b strings.Builder

	b.WriteString("\n" + m.renderHeader() + "\n\n")

	if m.showAgenda {
		b.WriteString(m.renderAgenda())
	} else {
		// grid: 2 cards per row, with a visible gap between columns
		for row := 0; row < len(dashboardCards); row += cardCols {
			var cells []string
			for col := 0; col < cardCols && row+col < len(dashboardCards); col++ {
				if col > 0 {
					cells = append(cells, cardGap)
				}
				cells = append(cells, m.renderCard(row+col))
			}
			rowBlock := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
			for _, l := range strings.Split(rowBlock, "\n") {
				b.WriteString(rowIndent + l + "\n")
			}
		}
	}

	b.WriteString("\n")
	// Both lines below are written unconditionally (blank when there's
	// nothing to show) so the view's total line count never changes
	// between frames. It used to skip the line entirely when m.err/
	// m.actionMsg was empty, which shrank the frame — with
	// tea.WithAltScreen(), bubbletea only repaints as many lines as the
	// *current* frame has, so a shorter frame left a stale line from the
	// previous, taller one sitting just below the new footer (looked like
	// a duplicated key bar).
	errLine := ""
	if m.err != nil {
		errLine = dashErrStyle.Render("⚠ last launch failed: " + m.err.Error())
	}
	b.WriteString(rowIndent + errLine + "\n")

	statusLine := ""
	if m.actionBusy {
		busyLabel := m.actionMsg
		if busyLabel == "" {
			busyLabel = "running…"
		}
		statusLine = dashMutedStyle.Render(busyLabel)
	} else if m.actionMsg != "" {
		style := dashOKStyle
		if strings.HasPrefix(m.actionMsg, "✗") {
			style = dashErrStyle
		}
		statusLine = style.Render(m.actionMsg)
	}
	b.WriteString(rowIndent + statusLine + "\n")

	// Pin the footer to the bottom of the screen instead of letting it
	// glue itself right under a short grid — pad the body out to the
	// terminal height first.
	if m.height > 0 {
		for lines := strings.Count(b.String(), "\n"); lines < m.height-1; lines++ {
			b.WriteString("\n")
		}
	}

	var footer string
	if m.showAgenda {
		footer = fmt.Sprintf(
			"%s dashboard  %s reload  %s quit",
			dashKeyStyle.Render("a"),
			dashKeyStyle.Render("r"),
			dashKeyStyle.Render("q"),
		)
	} else {
		xHint := ""
		if dashboardCards[m.cursor].action != nil {
			xHint = fmt.Sprintf("  %s quick action", dashKeyStyle.Render("x"))
		}
		loadHint := ""
		if m.loading {
			loadHint = "  " + dashMutedStyle.Render("↻ refreshing…")
		}
		footer = fmt.Sprintf(
			"%s move  %s open%s  %s search  %s sync  %s agenda  %s help  %s quit",
			dashKeyStyle.Render("↑↓←→"), dashKeyStyle.Render("enter"), xHint,
			dashKeyStyle.Render("/"), dashKeyStyle.Render("s"), dashKeyStyle.Render("a"),
			dashKeyStyle.Render("?"), dashKeyStyle.Render("q"),
		) + loadHint
	}
	b.WriteString(rowIndent + dashFootStyle.Render(footer) + "\n")

	return b.String()
}

func runDashboard(_ *cobra.Command, _ []string) error {
	p := tea.NewProgram(newDashboardModel())
	_, err := p.Run()
	return err
}

// ── cross-tool search ─────────────────────────────────────────────────────────

type searchResultMsg struct{ hits []SearchHit }

func runSearchCmd(q string) tea.Cmd {
	return func() tea.Msg {
		return searchResultMsg{hits: searchAll(context.Background(), q, 5, time.Now())}
	}
}

func (m dashboardModel) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if key == "esc" {
		m.showSearch = false
		m.searchInput.Blur()
		return m, nil
	}
	if m.searchTyping {
		switch key {
		case "enter":
			q := strings.TrimSpace(m.searchInput.Value())
			if q == "" || m.searchBusy {
				return m, nil
			}
			m.searchBusy = true
			return m, runSearchCmd(q)
		case "down":
			if len(m.searchHits) > 0 {
				m.searchTyping = false
				m.searchInput.Blur()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}
	// browsing results
	switch key {
	case "j", "down":
		if m.searchCursor < len(m.searchHits)-1 {
			m.searchCursor++
		}
	case "k", "up":
		if m.searchCursor > 0 {
			m.searchCursor--
		} else {
			m.searchTyping = true
			return m, m.searchInput.Focus()
		}
	case "/", "tab":
		m.searchTyping = true
		return m, m.searchInput.Focus()
	case "enter":
		if m.searchCursor < len(m.searchHits) {
			m.showSearch = false
			return m, m.launch(m.searchHits[m.searchCursor].Tool)
		}
	}
	return m, nil
}

func (m dashboardModel) renderSearch() string {
	var b strings.Builder
	b.WriteString("\n" + m.renderHeader() + "\n\n")
	b.WriteString(rowIndent + dashKeyStyle.Render("/ ") + m.searchInput.View() + "\n\n")
	switch {
	case m.searchBusy:
		b.WriteString(rowIndent + dashMutedStyle.Render("searching…") + "\n")
	case m.searchRan && len(m.searchHits) == 0:
		b.WriteString(rowIndent + dashMutedStyle.Render("Nothing found.") + "\n")
	default:
		last := ""
		for i, h := range m.searchHits {
			if h.Tool != last {
				b.WriteString("\n" + rowIndent + h.Icon + " " + dashKeyStyle.Render(h.Tool) + "\n")
				last = h.Tool
			}
			line := truncate(h.Title, max(m.width-16, 20))
			if h.When != "" {
				line += dashMutedStyle.Render("  " + h.When)
			}
			if h.Snippet != "" {
				line += dashMutedStyle.Render("  …" + truncate(h.Snippet, max(m.width/3, 12)) + "…")
			}
			prefix := "    "
			if !m.searchTyping && i == m.searchCursor {
				prefix = "  ▸ "
				line = lipgloss.NewStyle().Bold(true).Render(line)
			}
			b.WriteString(rowIndent + prefix + line + "\n")
		}
	}
	if m.height > 0 {
		for lines := strings.Count(b.String(), "\n"); lines < m.height-2; lines++ {
			b.WriteString("\n")
		}
	}
	foot := fmt.Sprintf("%s search  %s results  %s open tool  %s back",
		dashKeyStyle.Render("enter"), dashKeyStyle.Render("↑↓"), dashKeyStyle.Render("enter"), dashKeyStyle.Render("esc"))
	b.WriteString(rowIndent + dashFootStyle.Render(foot) + "\n")
	return b.String()
}
