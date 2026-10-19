package cmd

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func agendaStubs(now time.Time) map[string][]fakeRule {
	at := func(h int) string {
		return time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.Local).Format(time.RFC3339)
	}
	return map[string][]fakeRule{
		"calctl":  {{args: "list --today --format json", out: `{"data":[{"title":"Standup","start_time":"` + at(9) + `"},{"title":"Review","start_time":"` + at(15) + `"}]}`}},
		"taskctl": {{args: "today --json", out: `{"data":[{"title":"Pay rent","due_date":"` + time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Format(time.RFC3339) + `"}]}`}},
	}
}

func TestRunAgendaPrintsAllDayFirstThenTimed(t *testing.T) {
	now := time.Now()
	fakeSuite(t, agendaStubs(now))
	out := captureOut(t)
	if err := runAgenda(nil, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"Today's agenda", "Pay rent", "09:00", "Standup", "15:00", "Review"} {
		if !strings.Contains(s, want) {
			t.Errorf("agenda misses %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "Pay rent") > strings.Index(s, "Standup") || strings.Index(s, "Standup") > strings.Index(s, "Review") {
		t.Errorf("order: all-day first, then by time:\n%s", s)
	}
}

func TestRunAgendaEmptyDay(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	out := captureOut(t)
	if err := runAgenda(nil, nil); err != nil || !strings.Contains(out.String(), "Nothing scheduled today.") {
		t.Errorf("err=%v out=%q", err, out.String())
	}
}

func TestDashboardAgendaViewStates(t *testing.T) {
	now := time.Now()
	fakeSuite(t, agendaStubs(now))

	m := newDashboardModel()
	m.agendaLoad = true
	if got := ansi.Strip(m.renderAgenda()); !strings.Contains(got, "loading agenda…") {
		t.Errorf("loading: %q", got)
	}
	m.agendaLoad = false
	if got := ansi.Strip(m.renderAgenda()); !strings.Contains(got, "Nothing scheduled today.") {
		t.Errorf("empty: %q", got)
	}

	msg := loadAgendaCmd()().(agendaLoadedMsg)
	if len(msg.allDay) != 1 || len(msg.timed) != 2 || msg.timed[0].text != "Standup" {
		t.Fatalf("loaded: %+v", msg)
	}
	m.agendaAllDay, m.agendaTimed = msg.allDay, msg.timed
	got := ansi.Strip(m.renderAgenda())
	if !strings.Contains(got, "Pay rent") || !strings.Contains(got, "09:00") || !strings.Contains(got, "Review") {
		t.Errorf("filled:\n%s", got)
	}
}

func TestRefreshCmdLoadsEveryCardInParallel(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{
		"taskctl": {{args: "today --json", out: `{"overdue":0,"due_today":1,"data":[{"title":"X"}]}`}},
		"habctl":  {{args: "today --json", out: `{"done":1,"total":2,"data":[{"name":"Read","checked_today":false}]}`}},
	})
	isolateData(t)
	msg, ok := refreshCmd()().(cardsLoadedMsg)
	if !ok || len(msg.values) != len(dashboardCards) {
		t.Fatalf("one value per card: %v %d/%d", ok, len(msg.values), len(dashboardCards))
	}
	for i, c := range dashboardCards {
		switch c.tool {
		case "taskctl":
			if !strings.HasPrefix(msg.values[i].text, "1 due today") {
				t.Errorf("tasks card: %q", msg.values[i].text)
			}
		case "habctl":
			if !strings.HasPrefix(msg.values[i].text, "1/2 done today") {
				t.Errorf("habits card: %q", msg.values[i].text)
			}
		}
	}
	if msg.at.IsZero() {
		t.Error("the load time is recorded")
	}
}

func TestDashboardInitAndTick(t *testing.T) {
	m := newDashboardModel()
	if m.Init() == nil {
		t.Error("Init starts the first load")
	}
	if got := tickEvery(time.Millisecond)(); got == nil {
		t.Error("the tick produces a message")
	} else if _, ok := got.(tickMsg); !ok {
		t.Errorf("tick message type %T", got)
	}
	var _ tea.Cmd = tickEvery(time.Second)
}

func TestLaunchWithoutToolDoesNothing(t *testing.T) {
	if cmd := newDashboardModel().launch(""); cmd != nil {
		t.Error("a card without a jump target launches nothing")
	}
	if cmd := newDashboardModel().launch("taskctl"); cmd == nil {
		t.Error("a tool card launches its TUI")
	}
}
