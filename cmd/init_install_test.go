package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStdin feeds the wizard's prompts.
func withStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(text)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

// fakeRepo creates ~/Developing/Projects/missionctl/<project>/setup.sh for every tool; the script
// writes a marker file (or fails for the tools in failing).
func fakeRepo(t *testing.T, failing map[string]string) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	markers := t.TempDir()
	for _, tl := range tools {
		dir := filepath.Join(home, "Developing", "Projects", "missionctl", tl.project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := "touch '" + filepath.Join(markers, tl.name) + "'\n"
		if msg, bad := failing[tl.name]; bad {
			script = "echo '" + msg + "'\necho second line\nexit 1\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "setup.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return markers
}

func installed(markers string) map[string]bool {
	got := map[string]bool{}
	entries, _ := os.ReadDir(markers)
	for _, e := range entries {
		got[e.Name()] = true
	}
	return got
}

func TestShQuote(t *testing.T) {
	for in, want := range map[string]string{
		"plain":          "'plain'",
		"/Users/me/My V": "'/Users/me/My V'",
		"it's":           `'it'\''s'`,
		"$HOME `x`":      "'$HOME `x`'",
	} {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestInitWizardQuotesExportsForTheShell(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{}) // nothing installed
	t.Setenv("ANTHROPIC_API_KEY", "")
	withStdin(t, "sk-abc'def\n/Users/me/My Vault\nn\n")
	out := captureOut(t)
	if err := runInit(nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{`export ANTHROPIC_API_KEY='sk-abc'\''def'`, `export NOTECTL_VAULT_PATH='/Users/me/My Vault'`, "Tools not found: mailctl", "missionctl doctor"} {
		if !strings.Contains(s, want) {
			t.Errorf("output misses %q:\n%s", want, s)
		}
	}
}

func TestInitWizardSkipsWhatIsAlreadyThere(t *testing.T) {
	stubs := map[string][]fakeRule{}
	for _, tl := range tools {
		stubs[tl.name] = nil
	}
	fakeSuite(t, stubs)
	t.Setenv("ANTHROPIC_API_KEY", "already")
	withStdin(t, "\n") // only the vault prompt is asked: skip it
	out := captureOut(t)
	if err := runInit(nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "Already set.") || !strings.Contains(s, "All tools installed") || strings.Contains(s, "Add to your shell profile") {
		t.Errorf("output:\n%s", s)
	}
}

func TestInitWizardInstallsMissingToolsOnYes(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	markers := fakeRepo(t, map[string]string{"habctl": "go build failed"})
	t.Setenv("ANTHROPIC_API_KEY", "x")
	withStdin(t, "\ny\n")
	out := captureOut(t)
	if err := runInit(nil, nil); err != nil {
		t.Fatal(err)
	}
	got := installed(markers)
	if len(got) != len(tools)-1 || got["habctl"] || !got["taskctl"] {
		t.Errorf("setup.sh ran for: %v", got)
	}
	if !strings.Contains(out.String(), "Failed to install habctl: go build failed") {
		t.Errorf("the failing tool is reported with the first output line:\n%s", out)
	}
}

func TestInitWizardAnswerNoInstallsNothing(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	markers := fakeRepo(t, nil)
	t.Setenv("ANTHROPIC_API_KEY", "x")
	withStdin(t, "\nn\n")
	captureOut(t)
	if err := runInit(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := installed(markers); len(got) != 0 {
		t.Errorf("nothing may be installed on a 'no': %v", got)
	}
}

func TestRunInstallSkipsInstalledUnlessAll(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"taskctl": nil, "calctl": nil})
	markers := fakeRepo(t, nil)
	out := captureOut(t)
	installAll = false
	if err := runInstall(nil, nil); err != nil {
		t.Fatal(err)
	}
	got := installed(markers)
	if got["taskctl"] || got["calctl"] || len(got) != len(tools)-2 {
		t.Errorf("installed tools must be skipped: %v", got)
	}
	if !strings.Contains(out.String(), "9 tool(s) newly installed") || strings.Count(out.String(), "already installed") != 2 {
		t.Errorf("output:\n%s", out)
	}

	installAll = true
	t.Cleanup(func() { installAll = false })
	markers = fakeRepo(t, nil)
	captureOut(t)
	if err := runInstall(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := installed(markers); len(got) != len(tools) {
		t.Errorf("--all reinstalls every tool: %v", got)
	}
}

func TestRunInstallReportsFailuresAndContinues(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	markers := fakeRepo(t, map[string]string{"mailctl": "no Xcode", "postctl": "no Go"})
	out := captureOut(t)
	installAll = false
	err := runInstall(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "2 tool(s) failed to install") || !strings.Contains(err.Error(), "mailctl") || !strings.Contains(err.Error(), "postctl") {
		t.Fatalf("err = %v", err)
	}
	if got := installed(markers); len(got) != len(tools)-2 {
		t.Errorf("one failure must not stop the rest: %v", got)
	}
	if !strings.Contains(out.String(), "setup.sh failed: no Xcode") {
		t.Errorf("output:\n%s", out)
	}
}
