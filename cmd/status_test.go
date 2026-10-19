package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkDB creates the tool's database file (its real file name, in the throwaway data dir) with the given statements.
func mkDB(t *testing.T, tool string, stmts ...string) {
	t.Helper()
	path := resolvedToolDBPath(tool)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestOpenDBMissingFileIsNotAnError(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "nope.db"))
	if db != nil || err != nil {
		t.Errorf("a database that does not exist yet is 'not configured': %v %v", db, err)
	}
}

func TestNoteStatus(t *testing.T) {
	isolateData(t)
	if got := noteStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("no database: %q", got.text)
	}
	mkDB(t, "notectl", `CREATE TABLE other (x)`)
	if got := noteStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("a database without a notes table: %q", got.text)
	}

	isolateData(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	mkDB(t, "notectl",
		`CREATE TABLE notes (title TEXT, mod_time TEXT, created TEXT)`,
		`INSERT INTO notes VALUES ('Older', '2026-10-01T10:00:00Z', '2026-10-01T10:00:00Z')`,
		`INSERT INTO notes VALUES ('Newest', '2026-10-19T09:00:00Z', '2026-10-19T08:00:00Z')`,
		`INSERT INTO notes VALUES ('Today too', '2026-10-19T10:00:00Z', '2026-10-19T07:00:00Z')`,
		`INSERT INTO notes VALUES ('', '2026-10-19T11:00:00Z', '2026-10-19T11:00:00Z')`,
	)
	got := noteStatus(now)
	if got.text != "4 notes · 3 created today\nlatest: Today too" {
		t.Errorf("text = %q", got.text)
	}
	if strings.Join(got.items, "|") != "Today too|Newest|Older" {
		t.Errorf("items are newest first and skip untitled notes: %q", got.items)
	}
}

func TestNoteStatusKeepsAtMostTwelveItems(t *testing.T) {
	isolateData(t)
	stmts := []string{`CREATE TABLE notes (title TEXT, mod_time TEXT, created TEXT)`}
	for i := 0; i < 20; i++ {
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO notes VALUES ('n%02d', '2026-10-%02dT10:00:00Z', '2026-09-01')`, i, i+1))
	}
	mkDB(t, "notectl", stmts...)
	if got := noteStatus(time.Now()); len(got.items) != 12 || got.items[0] != "n19" {
		t.Errorf("items = %q", got.items)
	}
}

func TestMailStatus(t *testing.T) {
	isolateData(t)
	if got := mailStatus(); got.text != "–  not configured" {
		t.Errorf("no database: %q", got.text)
	}
	mkDB(t, "mailctl", `CREATE TABLE messages (subject TEXT, date TEXT, read INTEGER)`)
	if got := mailStatus(); got.text != "0 unread\ninbox clear" {
		t.Errorf("empty inbox: %q", got.text)
	}

	isolateData(t)
	mkDB(t, "mailctl",
		`CREATE TABLE messages (subject TEXT, date TEXT, read INTEGER)`,
		`INSERT INTO messages VALUES ('Old unread', '2026-10-01', 0)`,
		`INSERT INTO messages VALUES ('Latest unread', '2026-10-18', 0)`,
		`INSERT INTO messages VALUES ('Read one', '2026-10-19', 1)`,
	)
	if got := mailStatus(); got.text != "2 unread\nLatest unread" {
		t.Errorf("text = %q", got.text)
	}
	isolateData(t)
	mkDB(t, "mailctl", `CREATE TABLE messages (subject TEXT, date TEXT, read INTEGER)`, `INSERT INTO messages VALUES ('', '2026-10-18', 0)`)
	if got := mailStatus(); got.text != "1 unread" {
		t.Errorf("an unread mail without subject: %q", got.text)
	}
}

func TestDiaryStatusAt(t *testing.T) {
	isolateData(t)
	now := time.Date(2026, 10, 19, 15, 0, 0, 0, time.Local)
	if got := diaryStatusAt(now); got.text != "–  not configured" {
		t.Errorf("no database: %q", got.text)
	}
	mkDB(t, "diaryctl", `CREATE TABLE entries (date TEXT)`)
	if got := diaryStatusAt(now); got.text != "no entries\nwrite your first one" || got.urgency != urgencyWarn {
		t.Errorf("empty diary: %+v", got)
	}
	cases := []struct {
		last, want string
		urgency    urgencyLevel
	}{
		{"2026-10-19", "last entry: today\nyou're up to date", urgencyNormal},
		{"2026-10-18", "last entry: yesterday\nno entry yet today", urgencyWarn},
		{"2026-10-10", "last entry: Oct 10\nno entry yet today", urgencyWarn},
	}
	for _, c := range cases {
		isolateData(t)
		mkDB(t, "diaryctl", `CREATE TABLE entries (date TEXT)`, `INSERT INTO entries VALUES ('`+c.last+`')`)
		if got := diaryStatusAt(now); got.text != c.want || got.urgency != c.urgency {
			t.Errorf("last=%s → %q (%d), want %q (%d)", c.last, got.text, got.urgency, c.want, c.urgency)
		}
	}
}

func TestDiaryStatusUsesTheLocalCalendarDayJustAfterMidnight(t *testing.T) {
	// 00:30 local is still the previous day in UTC for a zone ahead of UTC; an entry from "yesterday"
	// must not be reported as today (this used the UTC date before).
	loc := time.FixedZone("CEST", 2*3600)
	now := time.Date(2026, 10, 20, 0, 30, 0, 0, loc)
	isolateData(t)
	mkDB(t, "diaryctl", `CREATE TABLE entries (date TEXT)`, `INSERT INTO entries VALUES ('2026-10-19')`)
	if got := diaryStatusAt(now); !strings.HasPrefix(got.text, "last entry: yesterday") {
		t.Errorf("got %q", got.text)
	}
}

func TestHabitStatus(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	if got := habitStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("not installed: %q", got.text)
	}
	cases := []struct{ json, want string }{
		{`{"done":0,"total":0,"data":[]}`, "no habits tracked"},
		{`{"done":1,"total":3,"data":[{"name":"Read","checked_today":true},{"name":"Walk","checked_today":false},{"name":"Stretch","checked_today":false}]}`, "1/3 done today\nnext: Walk"},
		{`{"done":2,"total":2,"data":[{"name":"Read","checked_today":true},{"name":"Walk","checked_today":true}]}`, "2/2 done today\nall done! 🎉"},
	}
	for _, c := range cases {
		fakeSuite(t, map[string][]fakeRule{"habctl": {{args: "today --json", out: c.json}}})
		if got := habitStatus(time.Now()); got.text != c.want {
			t.Errorf("%s → %q, want %q", c.json, got.text, c.want)
		}
	}
	fakeSuite(t, map[string][]fakeRule{"habctl": {{args: "today --json", out: `{"done":1,"total":2,"data":[{"name":"Read","checked_today":true},{"name":"Walk","checked_today":false}]}`}}})
	if got := habitStatus(time.Now()); strings.Join(got.items, "|") != "✓ Read|○ Walk" {
		t.Errorf("the drill-down marks done and open habits: %q", got.items)
	}
}

func TestTaskStatusBase(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	if got := taskStatusBase(); got.text != "–  not configured" {
		t.Errorf("not installed: %q", got.text)
	}
	cases := []struct {
		json, want string
		urgency    urgencyLevel
	}{
		{`{"overdue":0,"due_today":0,"data":[]}`, "nothing due today", urgencyNormal},
		{`{"overdue":0,"due_today":2,"data":[{"title":"Pay rent"}]}`, "2 due today\nPay rent", urgencyWarn},
		{`{"overdue":3,"due_today":1,"data":[{"title":"Call bank","due_date":"2026-10-17T00:00:00Z"}]}`, "1 due today · 3 overdue\nCall bank", urgencyCritical},
	}
	for _, c := range cases {
		fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "today --json", out: c.json}}})
		if got := taskStatusBase(); got.text != c.want || got.urgency != c.urgency {
			t.Errorf("%s → %q (%d), want %q (%d)", c.json, got.text, got.urgency, c.want, c.urgency)
		}
	}
	fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "today --json", out: `{"overdue":1,"due_today":0,"data":[{"title":"A","due_date":"2026-10-17T00:00:00Z"},{"title":"B"}]}`}}})
	if got := taskStatusBase(); strings.Join(got.items, "|") != "A  · due Oct 17|B" {
		t.Errorf("items = %q", got.items)
	}
}

func TestTaskStatusAddsASevenDaySparkline(t *testing.T) {
	now := time.Now()
	done := now.Format(time.RFC3339)
	fakeSuite(t, map[string][]fakeRule{"taskctl": {
		{args: "today --json", out: `{"overdue":0,"due_today":0,"data":[]}`},
		{args: "list --all --json", out: `{"data":[{"completed_at":"` + done + `"},{"completed_at":"` + done + `"},{"completed_at":null}]}`},
	}})
	got := taskStatus()
	if len(got.spark) != 7 || got.spark[6] != 2 {
		t.Errorf("two tasks completed today land in the last bucket: %v", got.spark)
	}
	fakeSuite(t, map[string][]fakeRule{})
	if got := taskStatus(); len(got.spark) != 0 {
		t.Errorf("no sparkline when the tool is missing: %v", got.spark)
	}
}

func TestRunStatusPrintsEveryAreaWithItsSyncAge(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{
		"taskctl": {{args: "today --json", out: `{"overdue":0,"due_today":0,"data":[]}`}},
		"habctl":  {{args: "today --json", out: `{"done":0,"total":0,"data":[]}`}},
	})
	isolateData(t)
	mkDB(t, "mailctl", `CREATE TABLE messages (subject TEXT, date TEXT, read INTEGER)`)
	if err := os.Chtimes(resolvedToolDBPath("mailctl"), time.Now().Add(-3*time.Hour), time.Now().Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	out := captureOut(t)
	if err := runStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"missionctl status", "Tasks", "nothing due today", "Calendar", "Timer", "Diary", "Budget", "Habits", "no habits tracked", "Notes", "Mail", "0 unread", "synced 3h ago", "not configured", "not synced yet"} {
		if !strings.Contains(s, want) {
			t.Errorf("status misses %q:\n%s", want, s)
		}
	}
}
