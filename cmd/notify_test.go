package cmd

import (
	"strings"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 10, 6, h, m, 0, 0, time.Local) }

func TestEventsAnnouncedInsideLeadTimeOnly(t *testing.T) {
	now := at(9, 55)
	c := notifyContext{Now: now, Timed: []agendaItem{
		{when: at(10, 0), hasTime: true, text: "Standup"},    // in 5 min → yes
		{when: at(10, 30), hasTime: true, text: "Review"},    // in 35 min → not yet
		{when: at(9, 20), hasTime: true, text: "Vorbei"},     // 35 min ago → no
		{when: at(9, 55), hasTime: true, text: "Jetzt"},      // exactly now → "now"
		{when: at(10, 1), text: "ohne Uhrzeit (hasTime=no)"}, // all-day style → never
	}}
	got := decideNotifications(c, nil)
	if len(got) != 2 {
		t.Fatalf("got %d notifications: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Body, "in 5 min — Standup") || !strings.HasPrefix(got[1].Body, "now — Jetzt") {
		t.Errorf("bodies: %q / %q", got[0].Body, got[1].Body)
	}
}

func TestEachNotificationOnlyOnce(t *testing.T) {
	c := notifyContext{Now: at(9, 55), Timed: []agendaItem{{when: at(10, 0), hasTime: true, text: "Standup"}}}
	first := decideNotifications(c, nil)
	seen := map[string]time.Time{first[0].Key: c.Now}
	if again := decideNotifications(c, seen); len(again) != 0 {
		t.Errorf("already announced, got %+v", again)
	}
	// a different event at the same time is its own notification
	c.Timed = append(c.Timed, agendaItem{when: at(10, 0), hasTime: true, text: "Zahnarzt"})
	if more := decideNotifications(c, seen); len(more) != 1 || !strings.Contains(more[0].Body, "Zahnarzt") {
		t.Errorf("want only the new event, got %+v", more)
	}
}

func TestHabitRiskOnlyInTheEveningAndOncePerDay(t *testing.T) {
	habits := []habitRisk{{"Sport", 4}}
	if got := decideNotifications(notifyContext{Now: at(14, 0), Habits: habits}, nil); len(got) != 0 {
		t.Errorf("before %d:00 nothing is at risk yet: %+v", habitRiskHour, got)
	}
	evening := notifyContext{Now: at(19, 0), Habits: habits}
	got := decideNotifications(evening, nil)
	if len(got) != 1 || !strings.Contains(got[0].Body, "Sport — 4-day streak") {
		t.Fatalf("evening: %+v", got)
	}
	if again := decideNotifications(evening, map[string]time.Time{got[0].Key: evening.Now}); len(again) != 0 {
		t.Error("same habit, same day → once")
	}
	next := notifyContext{Now: at(19, 0).AddDate(0, 0, 1), Habits: habits}
	if tomorrow := decideNotifications(next, map[string]time.Time{got[0].Key: evening.Now}); len(tomorrow) != 1 {
		t.Error("the next day is a new reminder")
	}
}

func TestOverdueDigestMorningOnly(t *testing.T) {
	if got := decideNotifications(notifyContext{Now: at(7, 30), DueCount: 3}, nil); len(got) != 0 {
		t.Errorf("too early: %+v", got)
	}
	if got := decideNotifications(notifyContext{Now: at(9, 0), DueCount: 0}, nil); len(got) != 0 {
		t.Errorf("nothing due: %+v", got)
	}
	got := decideNotifications(notifyContext{Now: at(9, 5), DueCount: 3}, nil)
	if len(got) != 1 || !strings.Contains(got[0].Body, "3 task(s)") {
		t.Errorf("digest: %+v", got)
	}
}

func TestSeenStatePersistsAndPrunes(t *testing.T) {
	path := t.TempDir() + "/sub/notified.json"
	now := at(12, 0)
	seen := map[string]time.Time{"old": now.Add(-72 * time.Hour), "fresh": now.Add(-time.Hour)}
	pruneSeen(seen, now)
	if _, ok := seen["old"]; ok || len(seen) != 1 {
		t.Errorf("prune: %v", seen)
	}
	if err := saveSeen(path, seen); err != nil {
		t.Fatal(err)
	}
	if got := loadSeen(path); len(got) != 1 || got["fresh"].IsZero() {
		t.Errorf("round trip: %v", got)
	}
	if got := loadSeen(t.TempDir() + "/missing.json"); got == nil || len(got) != 0 {
		t.Errorf("missing file must give an empty map, got %v", got)
	}
}

func TestNotifyPlist(t *testing.T) {
	p := notifyPlist("/usr/local/bin/missionctl", 300, "/tmp/logs")
	for _, want := range []string{"<string>sh.missionctl.notify</string>", "<string>/usr/local/bin/missionctl</string>",
		"<string>notify</string>", "<integer>300</integer>", "/tmp/logs/notify.log"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	if strings.Contains(p, "KeepAlive") {
		t.Error("an interval job must not be KeepAlive")
	}
}
