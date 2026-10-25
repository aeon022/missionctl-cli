package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var reviewNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)

func reviewSuite(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{
		"taskctl": {{args: "list --all --json", out: `{"data":[
			{"title":"Old done","status":"completed","completed_at":"2026-09-01T10:00:00Z"},
			{"title":"Ship receiptctl","status":"completed","completed_at":"2026-10-23T10:00:00Z"},
			{"title":"Write review","status":"completed","completed_at":"2026-10-22T10:00:00Z"},
			{"title":"Overdue","status":"needsAction","due_date":"2026-10-20T00:00:00Z"},
			{"title":"Future","status":"needsAction","due_date":"2026-11-20T00:00:00Z"}]}`}},
		"timectl": {{args: "log --json -d 7", out: `{"entries":[
			{"task":"a","project":"Alpha","duration_seconds":7200},
			{"task":"b","project":"Alpha","duration_seconds":1800},
			{"task":"c","project":"","duration_seconds":600}],"total_seconds":9600}`}},
		"habctl":    {{args: "stats --json --days 7", out: `{"data":[{"name":"Sport","streak":5,"days_done":6,"days_total":7},{"name":"Lesen","streak":2,"days_done":4,"days_total":7}]}`}},
		"healthctl": {{args: "adherence --json --days 7", out: `{"pct":92.5,"judged":14,"missed":1,"has_data":true}`}},
		"receiptctl":  {{args: "list --json", out: `{"data":[{"status":"ok","reviewed":true,"txn_ref":"x"},{"status":"unclear","reviewed":false,"txn_ref":""},{"status":"ok","reviewed":false,"txn_ref":""}]}`}},
		"budgetctl": {
			{args: "list --json -n 100000", out: `[{"Date":"2026-10-22T00:00:00Z","Amount":-120,"Category":"food"},{"Date":"2026-10-23T00:00:00Z","Amount":-30,"Category":"food"},{"Date":"2026-10-20T00:00:00Z","Amount":-50,"Category":""},{"Date":"2026-10-21T00:00:00Z","Amount":1000,"Category":"salary"},{"Date":"2026-01-01T00:00:00Z","Amount":-9999,"Category":"old"}]`},
			{args: "balance --json", out: `{"data":[{"account":"giro","anchored":true,"balance":2000}]}`},
		},
	})
}

func TestPlainReviewSumsEverythingLocally(t *testing.T) {
	reviewSuite(t)
	var b bytes.Buffer
	renderWeek(&b, gatherWeek(reviewNow, 7, false))
	got := b.String()
	for _, want := range []string{
		"last 7 days (2026-10-18 – 2026-10-24)",
		"2 done · 1 overdue", "Ship receiptctl, Write review",
		"2h 40m", "Alpha 2h 30m · c 10m",
		"Sport 6/7 · Lesen 4/7", "best streak 5 days",
		"92% of doses taken (1 missed)",
		"2 to check · 2 not matched to a booking (of 3)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Money") || strings.Contains(got, "€") {
		t.Errorf("money must stay out unless asked:\n%s", got)
	}
}

func TestPlainReviewMoneyOnlyWithFlagAndOnlyInTheWindow(t *testing.T) {
	reviewSuite(t)
	var b bytes.Buffer
	renderWeek(&b, gatherWeek(reviewNow, 7, true))
	got := b.String()
	for _, want := range []string{"spent €200 · income €1000", "top: food €150, (uncategorized) €50", "net worth"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "9999") {
		t.Errorf("a booking outside the window was counted:\n%s", got)
	}
}

func TestPlainReviewJSONHasNoMedicationNamesAndSkipsMissingTools(t *testing.T) {
	reviewSuite(t)
	var b bytes.Buffer
	if err := renderWeekJSON(&b, gatherWeek(reviewNow, 7, false)); err != nil {
		t.Fatal(err)
	}
	var back weekSummary
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || back.Health == nil || back.Health.Pct != 92.5 || back.Money != nil {
		t.Errorf("%v %+v\n%s", err, back, b.String())
	}

	fakeSuite(t, map[string][]fakeRule{"timectl": {{args: "log --json -d 7", out: `{"entries":[],"total_seconds":0}`}}})
	b.Reset()
	renderWeek(&b, gatherWeek(reviewNow, 7, true))
	if s := b.String(); !strings.Contains(s, "Time") || strings.Contains(s, "Tasks") || strings.Contains(s, "Habits") || strings.Contains(s, "Receipts") {
		t.Errorf("only installed tools may appear:\n%s", s)
	}
	fakeSuite(t, nil)
	b.Reset()
	renderWeek(&b, gatherWeek(reviewNow, 7, false))
	if !strings.Contains(b.String(), "no tool data available") {
		t.Errorf("no tools at all:\n%s", b.String())
	}
}

func TestReviewAIContextGetsReceiptCountsButNeverHealth(t *testing.T) {
	reviewSuite(t)
	c := gatherReviewContext(reviewNow)
	var receipts string
	for _, p := range c.Parts {
		if strings.HasPrefix(p.Title, "Medication") || strings.Contains(p.JSON, "92.5") {
			t.Errorf("health data must never reach an AI provider: %+v", p)
		}
		if strings.HasPrefix(p.Title, "Receipts") {
			receipts = p.JSON
		}
	}
	if !strings.Contains(receipts, `"unmatched":2`) || strings.Contains(receipts, "supplier") {
		t.Errorf("receipts: %q", receipts)
	}
}

func TestReviewFormatting(t *testing.T) {
	if euro(4.5) != "€4.50" || euro(150) != "€150" || euro(99.99) != "€99.99" {
		t.Error("euro formatting")
	}
	if plural(1, "day", "days") != "1 day" || plural(7, "day", "days") != "7 days" || plural(0, "day", "days") != "0 days" {
		t.Error("plural")
	}
}
