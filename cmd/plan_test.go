package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestBuildPlanPromptIncludesEverythingAndMarksEmpty(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 15, 0, 0, time.Local)
	c := planContext{
		Now:       now,
		AllDay:    []string{"Steuer fällig"},
		Timed:     []string{"10:00 Standup", "14:00 Zahnarzt"},
		OpenTasks: []string{"Rechnung schreiben"},
		Habits:    []string{"Sport (streak 4)"},
	}
	sys, user := buildPlanPrompt(c, "auto")
	for _, want := range []string{"Tuesday 2026-10-06 09:15", "Steuer fällig", "10:00 Standup", "14:00 Zahnarzt", "Rechnung schreiben", "Sport (streak 4)"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q:\n%s", want, user)
		}
	}
	if !strings.Contains(sys, "never schedule before") || !strings.Contains(sys, "language the items are written in") {
		t.Errorf("system prompt lost its rules/auto language:\n%s", sys)
	}

	_, empty := buildPlanPrompt(planContext{Now: now}, "auto")
	if strings.Count(empty, "(none)") != 4 {
		t.Errorf("each empty section must say (none), got:\n%s", empty)
	}
}

func TestPlanAndReviewLanguageDirective(t *testing.T) {
	sys, _ := buildPlanPrompt(planContext{Now: time.Now()}, "German")
	if !strings.HasSuffix(sys, "Answer in German.") {
		t.Errorf("explicit language not appended: %q", sys[len(sys)-40:])
	}
	rsys, _ := buildReviewPrompt(reviewContext{Now: time.Now()}, "German")
	if !strings.HasSuffix(rsys, "Answer in German.") {
		t.Errorf("review language: %q", rsys[len(rsys)-40:])
	}
}

func TestBuildReviewPromptSectionsAndNoData(t *testing.T) {
	c := reviewContext{Now: time.Date(2026, 10, 9, 18, 0, 0, 0, time.Local), Parts: []reviewPart{
		{"Time tracked (last 7 days)", `{"data":[{"project":"X","minutes":90}]}`},
		{"Habits today / streaks", `{"done":1,"total":2}`},
	}}
	_, user := buildReviewPrompt(c, "auto")
	for _, want := range []string{"Week ending Friday 2026-10-09", "## Time tracked (last 7 days)", `"minutes":90`, "## Habits today / streaks"} {
		if !strings.Contains(user, want) {
			t.Errorf("review prompt missing %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "## Budget") {
		t.Error("tools without data must not appear as sections")
	}
	_, none := buildReviewPrompt(reviewContext{Now: time.Now()}, "auto")
	if !strings.Contains(none, "(no tool data available)") {
		t.Errorf("empty review: %q", none)
	}
}
