package cmd

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// License settings screen — a full-screen mode toggled with "L" from the
// dashboard grid, so a Bundle buyer can paste their key once instead of
// running `<tool> license activate` six separate times. Drives the exact
// same activateAll/statusAll functions the `missionctl license` CLI
// command uses (see license.go), just rendered interactively.

type settingsStatusMsg []toolResult
type settingsActivateMsg []toolResult

func loadSettingsStatusCmd() tea.Cmd {
	return func() tea.Msg {
		return settingsStatusMsg(statusAll())
	}
}

func activateSettingsCmd(key string) tea.Cmd {
	return func() tea.Msg {
		return settingsActivateMsg(activateAll(key))
	}
}

// updateSettings handles all key input while the settings screen is open —
// called instead of the grid's key switch (see dashboardModel.Update).
func (m dashboardModel) updateSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.showSettings = false
		m.settingsInput.Blur()
		return m, nil
	case "r":
		if m.settingsBusy {
			return m, nil
		}
		m.settingsBusy = true
		m.settingsMsg = ""
		return m, loadSettingsStatusCmd()
	case "enter":
		key := strings.TrimSpace(m.settingsInput.Value())
		if key == "" || m.settingsBusy {
			return m, nil
		}
		m.settingsBusy = true
		m.settingsMsg = "activating…"
		return m, activateSettingsCmd(key)
	}

	var cmd tea.Cmd
	m.settingsInput, cmd = m.settingsInput.Update(msg)
	return m, cmd
}

// settingsMsgUpdate handles the two async message types settings.go's
// commands produce — called from dashboardModel.Update's main switch
// alongside its other message cases.
func (m dashboardModel) settingsMsgUpdate(msg tea.Msg) (dashboardModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case settingsStatusMsg:
		m.settingsBusy = false
		m.settingsResults = msg
		return m, nil, true
	case settingsActivateMsg:
		m.settingsBusy = false
		m.settingsResults = msg
		failed := 0
		for _, r := range msg {
			if r.installed && !r.ok {
				failed++
			}
		}
		switch {
		case failed == 0:
			m.settingsMsg = "✓ Bundle unlocked on every installed tool."
		default:
			m.settingsMsg = "✗ some tools failed — see details below, try again shortly if rate-limited."
		}
		m.settingsInput.SetValue("")
		return m, nil, true
	}
	return m, nil, false
}

func (m dashboardModel) renderSettings() string {
	w := m.width
	if w < 50 {
		w = 50
	}
	contentW := w - len(rowIndent)

	var b strings.Builder
	b.WriteString("\n" + rowIndent + dashTitleStyle.Render("🔑  LICENSE SETTINGS") + "\n")
	b.WriteString(rowIndent + dashRuleStyle.Render(strings.Repeat("─", contentW)) + "\n\n")

	b.WriteString(rowIndent + dashMutedStyle.Render("A Bundle key unlocks Pro features on every installed tool at once.") + "\n\n")

	nameStyle := lipgloss.NewStyle().Width(14)
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render("✓")
	crossMark := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("✗")
	dashMark := lipgloss.NewStyle().Foreground(dashSubtle).Render("–")

	if m.settingsBusy && len(m.settingsResults) == 0 {
		b.WriteString(rowIndent + dashMutedStyle.Render("checking…") + "\n")
	}
	for _, r := range m.settingsResults {
		mark := dashMark
		switch {
		case !r.installed:
			mark = dashMark
		case r.ok:
			mark = checkMark
		default:
			mark = crossMark
		}
		b.WriteString(rowIndent + nameStyle.Render(r.tool) + " " + mark + "  " + dashMutedStyle.Render(r.detail) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(rowIndent + dashMutedStyle.Render("Bundle key:") + "\n")
	b.WriteString(rowIndent + m.settingsInput.View() + "\n\n")

	if m.settingsMsg != "" {
		style := dashOKStyle
		if strings.HasPrefix(m.settingsMsg, "✗") {
			style = dashErrStyle
		} else if m.settingsBusy {
			style = dashMutedStyle
		}
		b.WriteString(rowIndent + style.Render(m.settingsMsg) + "\n\n")
	} else {
		b.WriteString("\n")
	}

	footer := lipgloss.JoinHorizontal(lipgloss.Top,
		dashKeyStyle.Render("enter"), dashFootStyle.Render(" activate  "),
		dashKeyStyle.Render("r"), dashFootStyle.Render(" refresh status  "),
		dashKeyStyle.Render("esc"), dashFootStyle.Render(" back"),
	)
	b.WriteString(rowIndent + footer + "\n")

	return b.String()
}
