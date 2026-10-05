package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// captureOut redirects cliOut (colorprofile writer: strips ANSI for a non-TTY
// buffer) for one test and returns what was written.
func captureOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := cliOut
	cliOut = colorprofile.NewWriter(&buf, nil)
	t.Cleanup(func() { cliOut = old })
	return &buf
}

// stubTools puts fake tool binaries on PATH that print the given stdout.
// A nil/"" body makes the stub exit 1 (tool failing).
func stubTools(t *testing.T, outs map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range outs {
		script := "#!/bin/sh\nexit 1\n"
		if body != "" {
			script = "#!/bin/sh\ncat <<'EOF'\n" + body + "\nEOF\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestBuildAgendaItems(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	ev := func(title string, h int) map[string]any {
		return map[string]any{"title": title, "start_time": time.Date(2026, 10, 5, h, 0, 0, 0, time.Local)}
	}
	task := func(title string, due *time.Time) map[string]any {
		return map[string]any{"title": title, "due_date": due}
	}
	day := func(d, h, m int) *time.Time { x := time.Date(2026, 10, d, h, m, 0, 0, time.Local); return &x }
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }

	stubTools(t, map[string]string{
		// local echo + synced copy of the same event → must collapse to one
		"calctl": j(map[string]any{"data": []any{ev("Standup", 9), ev("Standup", 9), ev("Review", 15)}}),
		"taskctl": j(map[string]any{"data": []any{
			task("Steuer", day(4, 0, 0)),          // yesterday, date-only → overdue, no time
			task("Heute ohne Zeit", day(5, 0, 0)), // today 00:00 is "before now" but not overdue
			task("Anruf", day(5, 9, 30)),          // has a real time
			task("Ohne Datum", nil),               // skipped
		}}),
		"timectl": j(map[string]any{"entries": []any{
			map[string]any{"task": "Code", "project": "missionctl", "started_at": "2026-10-05T08:00:00+02:00", "running": true},
			map[string]any{"task": "Kaputt", "started_at": "gestern"}, // unparsable → skipped
		}}),
	})

	items := buildAgendaItems(now)
	got := map[string]agendaItem{}
	for _, it := range items {
		got[it.text] = it
	}
	if len(items) != 6 {
		t.Fatalf("got %d items %v, want 6 (2 events, 3 tasks, 1 timer)", len(items), got)
	}
	for _, want := range []string{"Standup", "Review", "Steuer (overdue)", "Heute ohne Zeit", "Anruf", "Code (missionctl) — running"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	if got["Steuer (overdue)"].hasTime || got["Heute ohne Zeit"].hasTime {
		t.Error("date-only task due dates must not count as timed")
	}
	if !got["Anruf"].hasTime || !got["Code (missionctl) — running"].hasTime {
		t.Error("09:30 task and timer start must count as timed")
	}

	allDay, timed := splitAgendaItems(items)
	if len(allDay) != 2 || len(timed) != 4 {
		t.Fatalf("split = %d/%d, want 2/4", len(allDay), len(timed))
	}
	for i := 1; i < len(timed); i++ {
		if timed[i].when.Before(timed[i-1].when) {
			t.Errorf("timed not chronological: %v before %v", timed[i].when, timed[i-1].when)
		}
	}
}

func TestBuildAgendaItemsToolsFailing(t *testing.T) {
	stubTools(t, map[string]string{"calctl": "", "taskctl": "not json", "timectl": ""})
	if items := buildAgendaItems(time.Now()); len(items) != 0 {
		t.Errorf("failing/garbled tools must degrade to an empty agenda, got %v", items)
	}
}

func TestResultLine(t *testing.T) {
	out := "\n  activating…\n✓ calctl: active\n✗ Activation failed: bad key\n\n"
	cases := []struct{ prefix, want string }{
		{"✓", "✓ calctl: active"},
		{"License Type", "✗ Activation failed: bad key"}, // prefix missing → last non-empty line
		{"", "✗ Activation failed: bad key"},
	}
	for _, c := range cases {
		if got := resultLine(out, c.prefix); got != c.want {
			t.Errorf("resultLine(%q) = %q, want %q", c.prefix, got, c.want)
		}
	}
	if got := resultLine(" \n\n", "x"); got != "" {
		t.Errorf("blank output = %q", got)
	}
}

func TestFirstLineAndFormatAge(t *testing.T) {
	if firstLine("a\nb") != "a" || firstLine("solo") != "solo" || firstLine("") != "" {
		t.Error("firstLine")
	}
	cases := map[time.Duration]string{
		30 * time.Second:                "just now",
		59*time.Minute + 59*time.Second: "59m",
		time.Hour:                       "1h",
		23*time.Hour + 59*time.Minute:   "23h",
		24 * time.Hour:                  "1d",
		50 * time.Hour:                  "2d",
	}
	for d, want := range cases {
		if got := formatAge(d); got != want {
			t.Errorf("formatAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSyncAge(t *testing.T) {
	if got := syncAge("postctl"); got != "" {
		t.Errorf("tool without DB = %q, want empty", got)
	}
	dir := t.TempDir()
	t.Setenv("TASKCTL_DATA_DIR", dir)
	if got := syncAge("taskctl"); got != "not synced yet" {
		t.Errorf("missing DB = %q", got)
	}
	db := filepath.Join(dir, "taskctl.db") // DATA_DIR + base name of the default path
	os.WriteFile(db, nil, 0o644)
	old := time.Now().Add(-3 * time.Hour)
	os.Chtimes(db, old, old)
	if got := syncAge("taskctl"); got != "3h ago" {
		t.Errorf("syncAge = %q, want 3h ago", got)
	}
}

func TestCheckMCPConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd, _ := os.Getwd()
	cfg := map[string]any{
		"mcpServers": map[string]any{"calctl": map[string]any{}},
		"projects":   map[string]any{cwd: map[string]any{"mcpServers": map[string]any{"taskctl": map[string]any{}}}},
	}
	b, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(home, ".claude.json"), b, 0o644)

	buf := captureOut(t)
	s := lipgloss.NewStyle()
	checkMCPConfig("OK", "NO", s, s)
	out := buf.String()
	for _, want := range []string{"calctl OK  registered", "taskctl OK  registered", "mailctl NO  not registered — add: claude mcp add mailctl -- mailctl mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckMCPConfigMissingAndCorrupt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := lipgloss.NewStyle()

	buf := captureOut(t)
	checkMCPConfig("OK", "NO", s, s)
	if !strings.Contains(buf.String(), "not found") {
		t.Errorf("missing file: %s", buf.String())
	}
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{nope"), 0o644)
	buf.Reset()
	checkMCPConfig("OK", "NO", s, s)
	if !strings.Contains(buf.String(), "could not be parsed") {
		t.Errorf("corrupt file: %s", buf.String())
	}
}

func TestCheckDatabases(t *testing.T) {
	root := t.TempDir()
	for _, name := range toolDBOrder {
		t.Setenv(strings.ToUpper(name)+"_DATA_DIR", filepath.Join(root, name))
	}
	// only habctl has a DB, synced 2 days ago
	os.MkdirAll(filepath.Join(root, "habctl"), 0o755)
	db := filepath.Join(root, "habctl", "habits.db")
	os.WriteFile(db, nil, 0o644)
	old := time.Now().Add(-49 * time.Hour)
	os.Chtimes(db, old, old)

	buf := captureOut(t)
	s := lipgloss.NewStyle()
	checkDatabases("OK", "--", s, s)
	out := buf.String()
	if !strings.Contains(out, "habctl OK  last synced 2d ago") {
		t.Errorf("habctl line wrong:\n%s", out)
	}
	if !strings.Contains(out, "mailctl --  not created yet") {
		t.Errorf("mailctl line wrong:\n%s", out)
	}
}

func TestRunToolJSON(t *testing.T) {
	stubTools(t, map[string]string{"okctl": `{"n": 7}`, "badctl": "<html>", "failctl": ""})
	var v struct{ N int }
	if !runToolJSON("okctl", nil, &v) || v.N != 7 {
		t.Errorf("good tool: ok/N = %v", v)
	}
	if runToolJSON("badctl", nil, &v) || runToolJSON("failctl", nil, &v) || runToolJSON("nonexistent-tool-xyz", nil, &v) {
		t.Error("garbled output, non-zero exit and missing binary must all report false")
	}
}

// stubScript installs a fake tool whose full shell body is given, so a test
// can answer differently per subcommand ($1).
func stubScript(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestBudgetStatus(t *testing.T) {
	// goal list answers → alert branches
	cases := []struct {
		name, goals string
		wantText    string
		wantUrgency urgencyLevel
	}{
		{"near budget", `{"alerts":1,"data":[{"Category":"Food","Percent":85},{"Category":"Fun","Percent":40}]}`, "1 goal(s) over/near budget\nFood at 85%", urgencyWarn},
		{"blown", `{"alerts":2,"data":[{"Category":"Food","Percent":85},{"Category":"Fun","Percent":120}]}`, "2 goal(s) over/near budget\nFun at 120%", urgencyCritical},
		{"on track", `{"alerts":0,"data":[{"Category":"Food","Percent":30},{"Category":"Fun","Percent":10}]}`, "2 goals on track\nhighest: Food at 30%", urgencyNormal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubTools(t, map[string]string{"budgetctl": c.goals})
			got := budgetStatus(time.Now())
			if got.text != c.wantText || got.urgency != c.wantUrgency {
				t.Errorf("got %q/%v, want %q/%v", got.text, got.urgency, c.wantText, c.wantUrgency)
			}
		})
	}

	// no goals → falls back to the monthly summary; goal subcommand returns empty list
	stubScript(t, "budgetctl", `if [ "$1" = goal ]; then echo '{"alerts":0,"data":[]}'; else echo '{"Expenses":-250.4,"ByCategory":{"Food":-120,"Rent":-100,"":-30}}'; fi`)
	if got := budgetStatus(time.Now()).text; got != "€250 spent this month\ntop: Food (€120)" {
		t.Errorf("summary fallback = %q", got)
	}
	stubScript(t, "budgetctl", `if [ "$1" = goal ]; then exit 1; else echo '{"Expenses":-10,"ByCategory":{}}'; fi`)
	if got := budgetStatus(time.Now()).text; got != "€10 spent this month" {
		t.Errorf("no categories = %q", got)
	}
	stubTools(t, map[string]string{"budgetctl": ""})
	if got := budgetStatus(time.Now()).text; got != "–  not configured" {
		t.Errorf("tool failing = %q", got)
	}
}

func TestCalAndTimerStatus(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	at := func(h int) string { return time.Date(2026, 10, 5, h, 0, 0, 0, time.Local).Format(time.RFC3339) }

	stubTools(t, map[string]string{"calctl": `{"count":2,"data":[{"title":"Standup","start_time":"` + at(9) + `"},{"title":"Review","start_time":"` + at(15) + `"}]}`})
	if got := calStatus(now).text; got != "2 events today\nnext: Review at 15:00" {
		t.Errorf("calStatus = %q (past event must not be 'next')", got)
	}
	stubTools(t, map[string]string{"calctl": `{"count":1,"data":[{"title":"Standup","start_time":"` + at(9) + `"}]}`})
	if got := calStatus(now).text; got != "1 event today" {
		t.Errorf("all events past = %q", got)
	}
	stubTools(t, map[string]string{"calctl": `{"count":0,"data":[]}`})
	if got := calStatus(now).text; got != "no events today" {
		t.Errorf("empty = %q", got)
	}

	stubTools(t, map[string]string{"timectl": `{"total_human":"2h 10m","entries":[{"task":"Docs","running":false},{"task":"Code","project":"missionctl","running":true}]}`})
	if got := timerStatus(now).text; got != "running: Code (missionctl)\n2h 10m today" {
		t.Errorf("running timer = %q", got)
	}
	stubTools(t, map[string]string{"timectl": `{"total_human":"0m","entries":[]}`})
	if got := timerStatus(now).text; got != "no timer running\n0m today" {
		t.Errorf("idle timer = %q", got)
	}
}
