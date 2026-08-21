package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/theme"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type dashboardCard struct {
	key    string
	icon   string
	label  string
	tool   string // binary to launch on keypress; "" = no jump target
	color  lipgloss.Color
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
	now         time.Time
	actionMsg   string // result of the last "x" quick action, cleared on next action/refresh
	actionBusy  bool
	syncFailed  []string // syncableTools entries whose step errored, accumulated across a sync-all run

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
	ti.Width = 50

	m := dashboardModel{now: time.Now(), width: 80, settingsInput: ti}
	m.refresh()
	return m
}

func (m *dashboardModel) refresh() {
	m.now = time.Now()
	for i, c := range dashboardCards {
		m.values[i] = c.value(m.now)
	}
	m.lastRefresh = m.now
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m dashboardModel) Init() tea.Cmd {
	return tickEvery(time.Second)
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.settingsMsgUpdate(msg); handled {
		return next, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.MouseMsg:
		// The card grid always fits on screen — there's nothing to scroll.
		// Without mouse capture enabled, a trackpad/wheel scroll gets
		// translated by the terminal into arrow-key escapes instead, which
		// used to jump the card cursor around unintentionally. Capturing
		// mouse input turns that into a real MouseMsg here, which is simply
		// swallowed — scroll no longer does anything, on purpose.
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if m.now.Sub(m.lastRefresh) >= 30*time.Second {
			m.refresh()
		}
		return m, tickEvery(time.Second)

	case tea.KeyMsg:
		if m.showSettings {
			return m.updateSettings(msg)
		}
		switch msg.String() {
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
			m.refresh()
			return m, nil
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
		m.refresh()
		return m, nil

	case quickActionMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.actionMsg = "✗ " + msg.err.Error()
		} else {
			m.actionMsg = "✓ " + msg.result
			m.refresh()
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
		m.refresh()
		return m, nil

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
var (
	dashMuted         = theme.Muted
	dashSubtle        = theme.Subtle
	dashErrColor      = theme.Red
	dashWarnColor     = theme.Amber
	dashCriticalColor = theme.Red

	dashTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.OnAccent).
			Background(lipgloss.Color("57")).
			Padding(0, 2)

	dashTaglineStyle = lipgloss.NewStyle().Foreground(dashSubtle).Italic(true)
	dashRuleStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashClockStyle   = lipgloss.NewStyle().Foreground(dashMuted)
	dashFootStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashKeyStyle     = lipgloss.NewStyle().Foreground(theme.Amber).Bold(true)
	dashErrStyle     = lipgloss.NewStyle().Foreground(dashErrColor).Bold(true)
	dashOKStyle      = lipgloss.NewStyle().Foreground(theme.Green).Bold(true)
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

func (m dashboardModel) renderCard(i int) string {
	c := dashboardCards[i]
	w := m.cardWidth()
	selected := i == m.cursor

	var cardColor lipgloss.TerminalColor = c.color
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

func (m dashboardModel) View() string {
	if m.showSettings {
		return m.renderSettings()
	}

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
		footer = fmt.Sprintf(
			"%s/%s row  %s/%s column  %s or number jump in%s  %s refresh  %s sync all  %s agenda  %s license  %s quit",
			dashKeyStyle.Render("↑"), dashKeyStyle.Render("↓"),
			dashKeyStyle.Render("←"), dashKeyStyle.Render("→"),
			dashKeyStyle.Render("enter"), xHint,
			dashKeyStyle.Render("r"),
			dashKeyStyle.Render("s"),
			dashKeyStyle.Render("a"),
			dashKeyStyle.Render("L"),
			dashKeyStyle.Render("q"),
		)
	}
	b.WriteString(rowIndent + dashFootStyle.Render(footer) + "\n")

	return b.String()
}

func runDashboard(_ *cobra.Command, _ []string) error {
	p := tea.NewProgram(newDashboardModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
