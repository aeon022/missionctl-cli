package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(m dashboardModel, key string) (dashboardModel, tea.Cmd) {
	var msg tea.KeyPressMsg
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		msg = tea.KeyPressMsg{Text: key, Code: []rune(key)[0]}
	}
	mi, cmd := m.updateSettings(msg)
	return mi.(dashboardModel), cmd
}

func TestSettingsScreenMasksTheKeyAndActivates(t *testing.T) {
	calls := fakeSuite(t, bundleStubs())
	m := newDashboardModel()
	m.showSettings = true
	m.settingsInput.Focus()
	for _, r := range "KEY-123" {
		m, _ = press(m, string(r))
	}
	if view := ansi.Strip(m.renderSettings()); strings.Contains(view, "KEY-123") {
		t.Errorf("the typed licence key is visible on screen:\n%s", view)
	}

	m, cmd := press(m, "enter")
	if !m.settingsBusy || m.settingsMsg != "activating…" || cmd == nil {
		t.Fatalf("enter starts the activation: busy=%v msg=%q", m.settingsBusy, m.settingsMsg)
	}
	if again, c2 := press(m, "enter"); c2 != nil || !again.settingsBusy {
		t.Error("a second enter while busy must not start another activation")
	}
	m, _, handled := m.settingsMsgUpdate(cmd())
	if !handled || m.settingsBusy || m.settingsMsg != "✓ Bundle unlocked on every installed tool." || m.settingsInput.Value() != "" {
		t.Errorf("after success: busy=%v msg=%q key left=%q", m.settingsBusy, m.settingsMsg, m.settingsInput.Value())
	}
	got := strings.Join(calls(), "\n")
	if strings.Count(got, "stdin=<KEY-123>") != 5 || strings.Contains(got, "<activate> <KEY-123>") {
		t.Errorf("the key goes to each tool on stdin only, calls = %v", calls())
	}
}

func TestSettingsScreenReportsFailuresAndClearsTheKey(t *testing.T) {
	stubs := bundleStubs()
	stubs["notectl"] = []fakeRule{{args: "license activate --help", out: "standard input"}, {args: "license activate -", code: 1}}
	fakeSuite(t, stubs)
	m := newDashboardModel()
	m.settingsInput.SetValue("BAD")
	m, cmd := press(m, "enter")
	m, _, _ = m.settingsMsgUpdate(cmd())
	if !strings.HasPrefix(m.settingsMsg, "✗ some tools failed") {
		t.Errorf("msg = %q", m.settingsMsg)
	}
	if m.settingsInput.Value() != "" {
		t.Error("the key is cleared from the input even after a failure")
	}
}

func TestSettingsScreenStatusRefreshAndEscape(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"calctl": {{args: "license status", out: "License Type: PRO"}}})
	m := newDashboardModel()
	m.showSettings = true
	m, cmd := press(m, "r")
	if !m.settingsBusy || cmd == nil {
		t.Fatal("r refreshes the status")
	}
	m, _, _ = m.settingsMsgUpdate(cmd())
	if len(m.settingsResults) != len(licensedTools) || m.settingsBusy {
		t.Errorf("results = %d busy=%v", len(m.settingsResults), m.settingsBusy)
	}
	if view := ansi.Strip(m.renderSettings()); !strings.Contains(view, "License Type: PRO") || !strings.Contains(view, "LICENSE SETTINGS") {
		t.Errorf("view:\n%s", view)
	}
	m, _ = press(m, "esc")
	if m.showSettings {
		t.Error("esc closes the screen")
	}
	if m2, c := press(newDashboardModel(), "enter"); c != nil || m2.settingsBusy {
		t.Error("enter with an empty key does nothing")
	}
}

// --- missionctl update (source checkouts) ---

func fakeCheckouts(t *testing.T) (markers string) {
	t.Helper()
	markers = fakeRepo(t, nil)
	home, _ := os.UserHomeDir()
	for _, tl := range tools {
		if err := os.MkdirAll(filepath.Join(home, "Developing", "Projects", "missionctl", tl.project, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return markers
}

func TestUpdatePullsAndRebuildsEveryCheckout(t *testing.T) {
	calls := fakeSuite(t, map[string][]fakeRule{"git": nil})
	markers := fakeCheckouts(t)
	out := captureOut(t)
	updateCheckOnly, updateBinaries = false, false
	if err := runUpdate(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	if got := installed(markers); len(got) != len(tools) {
		t.Errorf("setup.sh ran for %d tools", len(got))
	}
	pulls := 0
	for _, c := range calls() {
		if strings.HasSuffix(c, "<pull> <--ff-only>") && strings.HasPrefix(c, "git <-C>") {
			pulls++
		}
	}
	if pulls != len(tools) || !strings.Contains(out.String(), "11/11 tools updated") {
		t.Errorf("pulls=%d\n%s", pulls, out)
	}
}

func TestUpdateCheckOnlyChangesNothing(t *testing.T) {
	calls := fakeSuite(t, map[string][]fakeRule{"git": nil})
	markers := fakeCheckouts(t)
	out := captureOut(t)
	updateCheckOnly = true
	t.Cleanup(func() { updateCheckOnly = false })
	if err := runUpdate(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	if len(calls()) != 0 || len(installed(markers)) != 0 {
		t.Errorf("--check must not pull or build: calls=%v", calls())
	}
	if !strings.Contains(out.String(), "11/11 tools checked") {
		t.Errorf("output:\n%s", out)
	}
}

func TestUpdateReportsAFailedPullAndKeepsGoing(t *testing.T) {
	home := t.TempDir() // fakeSuite replaces HOME; build the failing rule from the final one below
	_ = home
	fakeSuite(t, map[string][]fakeRule{})
	markers := fakeCheckouts(t)
	h, _ := os.UserHomeDir()
	bad := filepath.Join(h, "Developing", "Projects", "missionctl", "mailctl")
	// replace git by a stub that fails only for mailctl
	script := "#!/bin/sh\ncase \"$2\" in '" + bad + "') echo 'fatal: Not possible to fast-forward'; exit 1;; esac\nexit 0\n"
	dir := strings.SplitN(os.Getenv("PATH"), ":", 2)[0]
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out := captureOut(t)
	updateCheckOnly, updateBinaries = false, false
	err := runUpdate(updateCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "1 tool(s) failed to update: [mailctl]") {
		t.Fatalf("err = %v", err)
	}
	if got := installed(markers); got["mailctl"] || len(got) != len(tools)-1 {
		t.Errorf("a failed pull skips that tool's rebuild only: %v", got)
	}
	if !strings.Contains(out.String(), "git pull failed: fatal: Not possible to fast-forward") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSetAndReport(t *testing.T) {
	out := captureOut(t)
	if err := setAndReport(nil, "done"); err != nil || !strings.Contains(out.String(), "done") {
		t.Errorf("err=%v out=%q", err, out.String())
	}
	out.Reset()
	if err := setAndReport(os.ErrPermission, "done"); err != os.ErrPermission || out.Len() != 0 {
		t.Errorf("an error is returned and nothing is claimed: %v %q", err, out.String())
	}
}
