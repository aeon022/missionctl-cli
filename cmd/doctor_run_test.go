package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// plainStyle renders the path/detail column without the 14-wide wrapping of nameStyle.
var plainStyle = lipgloss.NewStyle()

// isolateData points every tool's data dir at a throwaway folder so nothing reads real data.
func isolateData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range toolDBOrder {
		t.Setenv(strings.ToUpper(name)+"_DATA_DIR", dir)
	}
	return dir
}

func allToolStubs(doctorOut string, code int) map[string][]fakeRule {
	stubs := map[string][]fakeRule{}
	for _, tl := range tools {
		stubs[tl.name] = []fakeRule{{args: "doctor", out: doctorOut, code: code}}
	}
	return stubs
}

func writeClaudeJSON(t *testing.T, body string) {
	t.Helper()
	home, _ := os.UserHomeDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunDoctorHealthySetupPasses(t *testing.T) {
	fakeSuite(t, allToolStubs("✓ Database  ok\n\nAll checks passed.", 0))
	isolateData(t)
	t.Setenv("ANTHROPIC_API_KEY", "x")
	servers := make([]string, 0, len(mcpTools))
	for _, n := range mcpTools {
		servers = append(servers, `"`+n+`":{}`)
	}
	writeClaudeJSON(t, `{"mcpServers":{`+strings.Join(servers, ",")+`}}`)
	out := captureOut(t)

	if err := runDoctor(nil, nil); err != nil {
		t.Fatalf("a complete setup must pass: %v\n%s", err, out)
	}
	s := out.String()
	for _, want := range []string{"11/11 tools installed", "ANTHROPIC_API_KEY", "set", "not created yet", "all checks passed", "not installed — see `diaryctl daemon --install`"} {
		if !strings.Contains(s, want) {
			t.Errorf("output misses %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "registered") < len(mcpTools) || strings.Contains(s, "not registered") {
		t.Errorf("every tool should be registered:\n%s", s)
	}
}

func TestRunDoctorReportsMissingToolsWithInstallHint(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "doctor", out: "All checks passed."}}})
	isolateData(t)
	out := captureOut(t)

	err := runDoctor(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "10 tool(s) not found") {
		t.Fatalf("err = %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "1/11 tools installed") || !strings.Contains(s, "setup.sh") {
		t.Errorf("output:\n%s", s)
	}
	if !strings.Contains(s, "ANTHROPIC_API_KEY") {
		t.Errorf("the environment section must still be shown:\n%s", s)
	}
}

func TestRunDoctorShowsFailingSelfCheck(t *testing.T) {
	stubs := allToolStubs("All checks passed.", 0)
	stubs["budgetctl"] = []fakeRule{{args: "doctor", out: "✓ Config\n✗ Database   locked", code: 1}}
	fakeSuite(t, stubs)
	isolateData(t)
	out := captureOut(t)
	if err := runDoctor(nil, nil); err != nil {
		t.Fatalf("a failing self-check is reported, it does not fail the run: %v", err)
	}
	if !strings.Contains(out.String(), "✗ Database   locked") {
		t.Errorf("the failing line is the reason:\n%s", out)
	}
}

func TestCheckMCPConfigCases(t *testing.T) {
	cases := []struct {
		name, body, want string
		noFile           bool
	}{
		{name: "no file", noFile: true, want: "~/.claude.json not found"},
		{name: "broken json", body: `{nope`, want: "could not be parsed"},
		{name: "user scope", body: `{"mcpServers":{"taskctl":{}}}`, want: "taskctl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeSuite(t, map[string][]fakeRule{})
			if !c.noFile {
				writeClaudeJSON(t, c.body)
			}
			out := captureOut(t)
			checkMCPConfig(checkMark, crossMark, nameStyle, plainStyle)
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("want %q in:\n%s", c.want, out)
			}
		})
	}
	t.Run("project scope counts as registered", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{})
		cwd, _ := os.Getwd()
		writeClaudeJSON(t, `{"projects":{"`+cwd+`":{"mcpServers":{"habctl":{}}}}}`)
		out := captureOut(t)
		checkMCPConfig(checkMark, crossMark, nameStyle, plainStyle)
		for _, l := range strings.Split(out.String(), "\n") {
			if strings.Contains(l, "habctl") && !strings.Contains(l, "registered") || strings.Contains(l, "habctl") && strings.Contains(l, "not registered") {
				t.Errorf("habctl is registered for this project: %q", l)
			}
			if strings.Contains(l, "taskctl") && !strings.Contains(l, "not registered") {
				t.Errorf("taskctl is not registered: %q", l)
			}
		}
	})
}

func TestCheckDatabasesStates(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	dir := isolateData(t)

	good := filepath.Join(dir, filepath.Base(toolDB["habctl"]))
	db, err := sql.Open("sqlite", good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(toolDB["timectl"])), []byte("this is not a database, just text padding"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureOut(t)
	checkDatabases(checkMark, dashMark, nameStyle, plainStyle)
	for _, l := range strings.Split(out.String(), "\n") {
		switch {
		case strings.Contains(l, "habctl"):
			if !strings.Contains(l, "last synced") {
				t.Errorf("healthy database: %q", l)
			}
		case strings.Contains(l, "timectl"):
			if !strings.Contains(l, "integrity check:") {
				t.Errorf("a corrupt database must be flagged: %q", l)
			}
		case strings.Contains(l, "calctl"):
			if !strings.Contains(l, "not created yet") {
				t.Errorf("missing database: %q", l)
			}
		}
	}
}

func TestCheckDaemonsEveryState(t *testing.T) {
	const label = "sh.missionctl.diaryctl"
	launchctl := func(out string, code int) map[string][]fakeRule {
		return map[string][]fakeRule{"launchctl": {{args: "list " + label, out: out, code: code}, {args: "list com.taskctl.daemon", out: out, code: code}}}
	}
	cases := []struct {
		name, out string
		code      int
		plist     bool
		want      string
	}{
		{"not installed", "", 0, false, "not installed — see `diaryctl daemon --install`"},
		{"plist but not loaded", "", 1, true, "plist exists but not loaded"},
		{"last run failed", `"LastExitStatus" = 256; "OnDemand" = false;`, 0, true, "last run failed (exit 256)"},
		{"continuous but dead", `"LastExitStatus" = 0; "OnDemand" = false;`, 0, true, "loaded but not running"},
		{"running", `"PID" = 4242; "LastExitStatus" = 0; "OnDemand" = false;`, 0, true, "running (pid 4242)"},
		{"on demand, idle", `"LastExitStatus" = 0; "OnDemand" = true;`, 0, true, "scheduled, last run OK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeSuite(t, launchctl(c.out, c.code))
			if c.plist {
				home, _ := os.UserHomeDir()
				dir := filepath.Join(home, "Library", "LaunchAgents")
				os.MkdirAll(dir, 0o755)
				os.WriteFile(filepath.Join(dir, label+".plist"), []byte("<plist/>"), 0o600)
			}
			out := captureOut(t)
			checkDaemons(checkMark, crossMark, dashMark, nameStyle, plainStyle)
			var line string
			for _, l := range strings.Split(out.String(), "\n") {
				if strings.Contains(l, "diaryctl") {
					line = l
				}
			}
			if !strings.Contains(line, c.want) {
				t.Errorf("want %q in %q", c.want, line)
			}
		})
	}
}

func TestEnvironmentSectionMarksSetAndOptional(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	isolateData(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("TIMECTL_HOURLY_RATE", "80")
	out := captureOut(t)
	runDoctor(nil, nil)
	for _, l := range strings.Split(out.String(), "\n") {
		switch {
		case strings.Contains(l, "ANTHROPIC_API_KEY") && !strings.Contains(l, "not set"):
			t.Errorf("a missing required variable is flagged: %q", l)
		case strings.Contains(l, "TIMECTL_GOAL_HOURS") && !strings.Contains(l, "not set (optional)"):
			t.Errorf("optional: %q", l)
		case strings.Contains(l, "TIMECTL_HOURLY_RATE") && strings.Contains(l, "not set"):
			t.Errorf("set variable reported as missing: %q", l)
		}
	}
}
