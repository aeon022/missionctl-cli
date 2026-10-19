package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestQuickCompleteTask(t *testing.T) {
	today := `{"data":[{"title":"Buy milk & eggs","list":"Home Inbox"}]}`

	t.Run("completes the first due task and passes the title as one argument", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "today --json", out: today}}})
		msg, err := quickCompleteTask()
		if err != nil || msg != "completed: Buy milk & eggs" {
			t.Fatalf("msg=%q err=%v", msg, err)
		}
		want := "taskctl <done> <Buy milk & eggs> <--list> <Home Inbox>"
		if got := calls(); len(got) != 2 || got[1] != want {
			t.Errorf("calls = %q, want the second to be %q", got, want)
		}
	})
	t.Run("a title that looks like a flag or shell syntax stays a single argument", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "today --json", out: `{"data":[{"title":"--help; rm -rf $HOME","list":""}]}`}}})
		if _, err := quickCompleteTask(); err != nil {
			t.Fatal(err)
		}
		if got := calls(); len(got) != 2 || got[1] != "taskctl <done> <--help; rm -rf $HOME>" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("nothing due", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "today --json", out: `{"data":[]}`}}})
		if _, err := quickCompleteTask(); err == nil || err.Error() != "no due task to complete" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("the tool's own error message is shown", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{"taskctl": {
			{args: "today --json", out: today},
			{args: "done Buy milk & eggs --list Home Inbox", out: "  list not found\n", code: 1},
		}})
		if _, err := quickCompleteTask(); err == nil || err.Error() != "list not found" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("taskctl missing", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{})
		if _, err := quickCompleteTask(); err == nil {
			t.Error("a missing tool must be an error, not a panic")
		}
	})
}

func TestQuickCheckHabit(t *testing.T) {
	t.Run("checks the first unchecked habit", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"habctl": {{args: "today --json", out: `{"data":[{"name":"Read","checked_today":true},{"name":"Walk 8k steps","checked_today":false},{"name":"Stretch","checked_today":false}]}`}}})
		msg, err := quickCheckHabit()
		if err != nil || msg != "checked: Walk 8k steps" {
			t.Fatalf("msg=%q err=%v", msg, err)
		}
		if got := calls(); len(got) != 2 || got[1] != "habctl <check> <Walk 8k steps>" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("all done", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"habctl": {{args: "today --json", out: `{"data":[{"name":"Read","checked_today":true}]}`}}})
		if _, err := quickCheckHabit(); err == nil || err.Error() != "all habits already done today" {
			t.Errorf("err = %v", err)
		}
		if len(calls()) != 1 {
			t.Errorf("nothing must be written when all habits are done: %q", calls())
		}
	})
	t.Run("not available, and a failing check", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{})
		if _, err := quickCheckHabit(); err == nil || err.Error() != "habctl not available" {
			t.Errorf("err = %v", err)
		}
		fakeSuite(t, map[string][]fakeRule{"habctl": {
			{args: "today --json", out: `{"data":[{"name":"Read","checked_today":false}]}`},
			{args: "check Read", out: "database is locked", code: 1},
		}})
		if _, err := quickCheckHabit(); err == nil || err.Error() != "database is locked" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestQuickStopTimer(t *testing.T) {
	t.Run("stops a running timer", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"timectl": {{args: "today --json", out: `{"entries":[{"running":false},{"running":true}]}`}}})
		if msg, err := quickStopTimer(); err != nil || msg != "timer stopped" {
			t.Fatalf("msg=%q err=%v", msg, err)
		}
		if got := calls(); len(got) != 2 || got[1] != "timectl <stop>" {
			t.Errorf("calls = %q", got)
		}
	})
	t.Run("no timer running writes nothing", func(t *testing.T) {
		calls := fakeSuite(t, map[string][]fakeRule{"timectl": {{args: "today --json", out: `{"entries":[{"running":false}]}`}}})
		if _, err := quickStopTimer(); err == nil || err.Error() != "no timer running" {
			t.Errorf("err = %v", err)
		}
		if len(calls()) != 1 {
			t.Errorf("calls = %q", calls())
		}
	})
	t.Run("not available, failing stop", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{})
		if _, err := quickStopTimer(); err == nil || err.Error() != "timectl not available" {
			t.Errorf("err = %v", err)
		}
		fakeSuite(t, map[string][]fakeRule{"timectl": {
			{args: "today --json", out: `{"entries":[{"running":true}]}`},
			{args: "stop", out: "no timer running", code: 1},
		}})
		if _, err := quickStopTimer(); err == nil || err.Error() != "no timer running" {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRunQuickActionWrapsResultAndError(t *testing.T) {
	msg := runQuickAction(func() (string, error) { return "ok", nil })().(quickActionMsg)
	if msg.result != "ok" || msg.err != nil {
		t.Errorf("%+v", msg)
	}
	msg = runQuickAction(func() (string, error) { return "", errTest })().(quickActionMsg)
	if msg.err != errTest {
		t.Errorf("%+v", msg)
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }

const errTest = testErr("boom")

func TestHealthStatusShowsCountsNeverNames(t *testing.T) {
	cases := []struct {
		json, want string
		urgency    urgencyLevel
	}{
		{`{"Done":0,"Total":0,"Due":0,"Next":""}`, "no doses today", urgencyNormal},
		{`{"Done":1,"Total":3,"Due":2,"Next":"20:00"}`, "1/3 doses taken\n2 due now", urgencyWarn},
		{`{"Done":2,"Total":3,"Due":0,"Next":"20:00"}`, "2/3 doses taken\nnext 20:00", urgencyNormal},
		{`{"Done":3,"Total":3,"Due":0,"Next":""}`, "3/3 doses taken\nall done", urgencyNormal},
	}
	for _, c := range cases {
		fakeSuite(t, map[string][]fakeRule{"healthctl": {{args: "today --json", out: c.json}}})
		if got := healthStatus(time.Now()); got.text != c.want || got.urgency != c.urgency {
			t.Errorf("%s → %q (urgency %d), want %q (%d)", c.json, got.text, got.urgency, c.want, c.urgency)
		}
	}
	fakeSuite(t, map[string][]fakeRule{})
	if got := healthStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("not installed: %q", got.text)
	}
}

func TestInvestStatus(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"investctl": {{args: "summary --json", out: `{"data":{"totals":[{"Currency":"EUR","Value":12345.6,"DayChange":-12.345}]}}`}}})
	if got := investStatus(time.Now()); got.text != "12346 EUR\nday -12.35" {
		t.Errorf("got %q", got.text)
	}
	fakeSuite(t, map[string][]fakeRule{"investctl": {{args: "summary --json", out: `{"data":{"totals":[]}}`}}})
	if got := investStatus(time.Now()); got.text != "no holdings yet" {
		t.Errorf("got %q", got.text)
	}
	fakeSuite(t, map[string][]fakeRule{"investctl": {{args: "summary --json", code: 1}}})
	if got := investStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("a failing tool degrades to not configured: %q", got.text)
	}
}

func TestSyncStepRunsOneToolAndReportsItsError(t *testing.T) {
	calls := fakeSuite(t, map[string][]fakeRule{"taskctl": {{args: "sync"}}, "calctl": {{args: "sync", code: 1}}})
	if m := syncStepCmd(0)().(syncStepMsg); m.idx != 0 || m.err != nil {
		t.Errorf("taskctl: %+v", m)
	}
	if m := syncStepCmd(1)().(syncStepMsg); m.idx != 1 || m.err == nil {
		t.Errorf("a failing sync is reported: %+v", m)
	}
	if got := strings.Join(calls(), ","); got != "taskctl <sync>,calctl <sync>" {
		t.Errorf("calls = %s", got)
	}
	if got := syncStepLabel(1); got != "syncing calctl… (2/4)" {
		t.Errorf("label = %q", got)
	}
}
