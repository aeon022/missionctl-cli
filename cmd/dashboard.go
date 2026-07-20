package cmd

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type dashboardRow struct {
	key   string
	label string
	tool  string // binary to launch on keypress; "" = no jump target
	value func(now time.Time) string
}

var dashboardRows = []dashboardRow{
	{"1", "Tasks", "taskctl", func(_ time.Time) string { return taskStatus() }},
	{"2", "Calendar", "calctl", calStatus},
	{"3", "Timer", "timectl", timerStatus},
	{"4", "Diary", "diaryctl", func(_ time.Time) string { return diaryStatus() }},
	{"5", "Budget", "budgetctl", budgetStatus},
	{"6", "Habits", "habctl", habitStatus},
	{"7", "Notes", "notectl", noteStatus},
	{"8", "Mail", "mailctl", func(_ time.Time) string { return mailStatus() }},
}

type dashboardModel struct {
	cursor int
	err    error
}

func newDashboardModel() dashboardModel {
	return dashboardModel{}
}

func (m dashboardModel) Init() tea.Cmd { return nil }

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(dashboardRows)-1 {
				m.cursor++
			}
			return m, nil
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "enter":
			return m, m.launch(dashboardRows[m.cursor].tool)
		}
		for i, r := range dashboardRows {
			if msg.String() == r.key {
				m.cursor = i
				return m, m.launch(r.tool)
			}
		}
	case launchErrMsg:
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

type launchErrMsg struct{ err error }

// launch suspends the dashboard's terminal control, runs the target tool's
// TUI in the foreground, and resumes the dashboard once it exits.
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
	dashHeaderStyle   = lipgloss.NewStyle().Bold(true)
	dashKeyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dashLabelStyle    = lipgloss.NewStyle().Width(12)
	dashSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dashDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	dashErrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

func (m dashboardModel) View() string {
	now := time.Now()
	var b []byte
	b = append(b, []byte("\n  "+dashHeaderStyle.Render(fmt.Sprintf("missionctl — %s", now.Format("Mon Jan 02, 15:04")))+"\n\n")...)

	for i, r := range dashboardRows {
		cursor := "  "
		labelStyle := dashLabelStyle
		if i == m.cursor {
			cursor = dashSelectedStyle.Render("▸ ")
			labelStyle = dashLabelStyle.Bold(true)
		}
		key := dashDimStyle.Render("[" + r.key + "]")
		if r.tool == "" {
			key = dashDimStyle.Render("[ ]")
		}
		b = append(b, []byte(fmt.Sprintf("  %s%s %s  %s\n", cursor, key, labelStyle.Render(r.label), r.value(now)))...)
	}

	b = append(b, []byte("\n")...)
	if m.err != nil {
		b = append(b, []byte("  "+dashErrStyle.Render("last launch failed: "+m.err.Error())+"\n")...)
	}
	b = append(b, []byte(dashDimStyle.Render("  ↑/↓ move · 1-8 or enter jump into a tool · q quit")+"\n")...)

	return string(b)
}

func runDashboard(_ *cobra.Command, _ []string) error {
	p := tea.NewProgram(newDashboardModel())
	_, err := p.Run()
	return err
}
