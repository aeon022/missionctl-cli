package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type dashboardCard struct {
	key   string
	icon  string
	label string
	tool  string // binary to launch on keypress; "" = no jump target
	color lipgloss.Color
	value func(now time.Time) string
}

var dashboardCards = []dashboardCard{
	{"1", "✓", "Tasks", "taskctl", lipgloss.Color("39"), func(_ time.Time) string { return taskStatus() }},
	{"2", "📅", "Calendar", "calctl", lipgloss.Color("42"), calStatus},
	{"3", "⏱", "Timer", "timectl", lipgloss.Color("221"), timerStatus},
	{"4", "📔", "Diary", "diaryctl", lipgloss.Color("212"), func(_ time.Time) string { return diaryStatus() }},
	{"5", "💰", "Budget", "budgetctl", lipgloss.Color("208"), budgetStatus},
	{"6", "🔥", "Habits", "habctl", lipgloss.Color("203"), habitStatus},
	{"7", "📝", "Notes", "notectl", lipgloss.Color("135"), noteStatus},
	{"8", "✉", "Mail", "mailctl", lipgloss.Color("33"), func(_ time.Time) string { return mailStatus() }},
}

// Icons are plain Unicode emoji (not Nerd Font glyphs) so they render
// correctly in any modern terminal font, not just ones with icon patches.

type tickMsg time.Time

type dashboardModel struct {
	cursor      int
	err         error
	width       int
	height      int
	values      [8]string
	lastRefresh time.Time
	now         time.Time
}

func newDashboardModel() dashboardModel {
	m := dashboardModel{now: time.Now(), width: 80}
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if m.now.Sub(m.lastRefresh) >= 30*time.Second {
			m.refresh()
		}
		return m, tickEvery(time.Second)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor+cardCols < len(dashboardCards) {
				m.cursor += cardCols
			}
			return m, nil
		case "k", "up":
			if m.cursor-cardCols >= 0 {
				m.cursor -= cardCols
			}
			return m, nil
		case "h", "left":
			if m.cursor%cardCols != 0 {
				m.cursor--
			}
			return m, nil
		case "l", "right":
			if m.cursor%cardCols != cardCols-1 && m.cursor < len(dashboardCards)-1 {
				m.cursor++
			}
			return m, nil
		case "r":
			m.refresh()
			return m, nil
		case "enter":
			return m, m.launch(dashboardCards[m.cursor].tool)
		}
		for i, c := range dashboardCards {
			if msg.String() == c.key {
				m.cursor = i
				return m, m.launch(c.tool)
			}
		}

	case launchErrMsg:
		m.err = msg.err
		m.refresh()
		return m, nil
	}
	return m, nil
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

var (
	dashBg       = lipgloss.Color("235")
	dashFg       = lipgloss.Color("255")
	dashMuted    = lipgloss.Color("245")
	dashSubtle   = lipgloss.Color("240")
	dashErrColor = lipgloss.Color("203")

	dashTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(dashFg).
			Background(lipgloss.Color("57")).
			Padding(0, 2)

	dashTaglineStyle = lipgloss.NewStyle().Foreground(dashSubtle).Italic(true)
	dashRuleStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashClockStyle   = lipgloss.NewStyle().Foreground(dashMuted)
	dashFootStyle    = lipgloss.NewStyle().Foreground(dashSubtle)
	dashKeyStyle     = lipgloss.NewStyle().Foreground(dashFg).Bold(true)
	dashErrStyle     = lipgloss.NewStyle().Foreground(dashErrColor).Bold(true)
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

	border := lipgloss.RoundedBorder()
	borderColor := c.color
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(c.color)
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

	value := m.values[i]
	valueStyle := lipgloss.NewStyle().Foreground(dashFg).Width(w - 2)
	if value == "" || strings.HasPrefix(value, "–") {
		valueStyle = valueStyle.Foreground(dashSubtle)
	}

	body := headLine + "\n" + valueStyle.Render(truncate(value, (w-2)*2))

	box := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(w)

	return box.Render(body)
}

func truncate(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
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

func (m dashboardModel) View() string {
	var b strings.Builder

	b.WriteString("\n" + m.renderHeader() + "\n\n")

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

	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(rowIndent + dashErrStyle.Render("⚠ last launch failed: "+m.err.Error()) + "\n")
	}

	footer := fmt.Sprintf(
		"%s/%s row  %s/%s column  %s or number jump in  %s refresh  %s quit",
		dashKeyStyle.Render("↑"), dashKeyStyle.Render("↓"),
		dashKeyStyle.Render("←"), dashKeyStyle.Render("→"),
		dashKeyStyle.Render("enter"),
		dashKeyStyle.Render("r"),
		dashKeyStyle.Render("q"),
	)
	b.WriteString(rowIndent + dashFootStyle.Render(footer) + "\n")

	return b.String()
}

func runDashboard(_ *cobra.Command, _ []string) error {
	p := tea.NewProgram(newDashboardModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
