package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGatherPlanContextCollectsHabitsAndCapsTasks(t *testing.T) {
	var tasks []string
	for i := 1; i <= 20; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"title":"task %02d","list":"Inbox"}`, i))
	}
	calls := fakeSuite(t, map[string][]fakeRule{
		"habctl":  {{args: "today --json", out: `{"data":[{"name":"Read","checked_today":false,"streak":4},{"name":"Walk","checked_today":true,"streak":9}]}`}},
		"taskctl": {{args: "list --json", out: `{"data":[` + strings.Join(tasks, ",") + `]}`}},
	})
	c := gatherPlanContext(time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local))
	if len(c.Habits) != 1 || c.Habits[0] != "Read (streak 4)" {
		t.Errorf("only unchecked habits are planned: %q", c.Habits)
	}
	if len(c.OpenTasks) != 15 || c.OpenTasks[0] != "task 01" || c.OpenTasks[14] != "task 15" {
		t.Errorf("the prompt is capped at 15 tasks: %d %q", len(c.OpenTasks), c.OpenTasks)
	}
	for _, l := range calls() {
		if strings.HasPrefix(l, "healthctl") {
			t.Errorf("health data must never feed the AI planner: %q", l)
		}
	}
}

func TestGatherReviewContextSkipsMissingToolsTruncatesAndNeverReadsHealth(t *testing.T) {
	big := `{"data":"` + strings.Repeat("x", 5000) + `"}`
	calls := fakeSuite(t, map[string][]fakeRule{
		"timectl":   {{args: "log --json -d 7", out: `{"entries":[]}`}},
		"budgetctl": {{args: "summary --json", out: big}},
		"healthctl": {{args: "today --json", out: `{"Done":1,"Total":2}`}},
		"investctl": {{args: "summary --json", out: `{"data":{}}`}},
	})
	c := gatherReviewContext(time.Date(2026, 10, 19, 9, 0, 0, 0, time.Local))
	titles := []string{}
	for _, p := range c.Parts {
		titles = append(titles, p.Title)
	}
	if strings.Join(titles, "|") != "Time tracked (last 7 days)|Budget (current month)" {
		t.Errorf("only the installed tools appear, in order: %q", titles)
	}
	budget := c.Parts[1].JSON
	if len(budget) > reviewPartMax+len("…(truncated)") || !strings.HasSuffix(budget, "…(truncated)") {
		t.Errorf("a large snapshot is cut to %d bytes with a marker: %d", reviewPartMax, len(budget))
	}
	for _, l := range calls() {
		if strings.HasPrefix(l, "healthctl") || strings.HasPrefix(l, "investctl") {
			t.Errorf("health and investment data must never be sent to an AI provider: %q", l)
		}
	}
}

func TestReviewAndPlanPromptsExplainEmptyData(t *testing.T) {
	_, user := buildReviewPrompt(reviewContext{Now: time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local)}, "")
	if !strings.Contains(user, "(no tool data available)") || !strings.Contains(user, "Week ending Monday 2026-10-19") {
		t.Errorf("user prompt:\n%s", user)
	}
	sys, _ := buildReviewPrompt(reviewContext{}, "German")
	if !strings.HasSuffix(sys, "Answer in German.") {
		t.Errorf("language override: %q", sys[len(sys)-40:])
	}
}
