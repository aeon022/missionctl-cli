package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/ai"
	"github.com/spf13/cobra"
)

// planContext is everything the planner knows about today, already reduced to
// plain lines so the prompt builder stays a pure function.
type planContext struct {
	Now       time.Time
	AllDay    []string
	Timed     []string // "09:30 Standup"
	Habits    []string // unchecked today: "Sport (streak 4)"
	OpenTasks []string
}

func gatherPlanContext(now time.Time) planContext {
	c := planContext{Now: now}
	allDay, timed := splitAgendaItems(buildAgendaItems(now))
	for _, it := range allDay {
		c.AllDay = append(c.AllDay, it.text)
	}
	for _, it := range timed {
		c.Timed = append(c.Timed, it.when.Format("15:04")+" "+it.text)
	}

	var habits struct {
		Data []struct {
			Name         string `json:"name"`
			CheckedToday bool   `json:"checked_today"`
			Streak       int    `json:"streak"`
		} `json:"data"`
	}
	if runToolJSON("habctl", []string{"today", "--json"}, &habits) {
		for _, h := range habits.Data {
			if !h.CheckedToday {
				c.Habits = append(c.Habits, fmt.Sprintf("%s (streak %d)", h.Name, h.Streak))
			}
		}
	}

	var tasks struct {
		Data []struct {
			Title string `json:"title"`
			List  string `json:"list"`
		} `json:"data"`
	}
	if runToolJSON("taskctl", []string{"list", "--json"}, &tasks) {
		for i, t := range tasks.Data {
			if i >= 15 {
				break
			}
			c.OpenTasks = append(c.OpenTasks, t.Title)
		}
	}
	return c
}

const planSystem = `You are a calm, practical day planner. Produce a realistic plan for the rest of today
from the user's data. Rules: respect fixed calendar events exactly; put focus work in the
free gaps between them; never schedule before "now"; keep it short (one line per block,
"HH:MM–HH:MM  what"); name the single most important thing first; if there are more tasks
than time, say which to drop or move. Habits not yet done today are small anchors — slot
them in, don't pile them at the end. No preamble, no motivational filler.`

// buildPlanPrompt turns the context into the user prompt. lang "" or "auto"
// answers in the language the items are written in.
func buildPlanPrompt(c planContext, lang string) (system, user string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Now: %s\n\n", c.Now.Format("Monday 2006-01-02 15:04"))
	section := func(title string, lines []string) {
		fmt.Fprintf(&b, "%s:\n", title)
		if len(lines) == 0 {
			b.WriteString("  (none)\n\n")
			return
		}
		for _, l := range lines {
			b.WriteString("  - " + l + "\n")
		}
		b.WriteString("\n")
	}
	section("All-day / due today", c.AllDay)
	section("Calendar & scheduled (fixed)", c.Timed)
	section("Open tasks (unscheduled)", c.OpenTasks)
	section("Habits not done yet today", c.Habits)
	system = planSystem
	if lang != "" && lang != "auto" {
		system += "\nAnswer in " + lang + "."
	} else {
		system += "\nAnswer in the language the items are written in."
	}
	return system, b.String()
}

// reviewContext holds compact JSON snapshots from each tool for a weekly review.
type reviewContext struct {
	Now   time.Time
	Parts []reviewPart
}

type reviewPart struct{ Title, JSON string }

const reviewPartMax = 3500

func gatherReviewContext(now time.Time) reviewContext {
	c := reviewContext{Now: now}
	for _, src := range []struct {
		title, bin string
		args       []string
	}{
		{"Time tracked (last 7 days)", "timectl", []string{"log", "--json", "-d", "7"}},
		{"Tasks", "taskctl", []string{"week", "--json"}},
		{"Habits today / streaks", "habctl", []string{"today", "--json"}},
		{"Budget (current month)", "budgetctl", []string{"summary", "--json"}},
		{"Diary (recent entries)", "diaryctl", []string{"list", "--json", "--limit", "7"}},
	} {
		var v any
		if !runToolJSON(src.bin, src.args, &v) {
			continue
		}
		b, _ := json.Marshal(v)
		s := string(b)
		if len(s) > reviewPartMax {
			s = s[:reviewPartMax] + "…(truncated)"
		}
		c.Parts = append(c.Parts, reviewPart{src.title, s})
	}
	return c
}

const reviewSystem = `You write a short weekly review for one person from raw tool data. Structure:
1) What got done (tasks, time per project), 2) Habits & consistency, 3) Money in one line if
present, 4) Mood/themes from the diary if present, 5) Three concrete suggestions for next
week. Be specific with numbers from the data, never invent data that isn't there, and skip
any section whose data is missing. No preamble.`

func buildReviewPrompt(c reviewContext, lang string) (system, user string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Week ending %s\n\n", c.Now.Format("Monday 2006-01-02"))
	if len(c.Parts) == 0 {
		b.WriteString("(no tool data available)\n")
	}
	for _, p := range c.Parts {
		fmt.Fprintf(&b, "## %s\n%s\n\n", p.Title, p.JSON)
	}
	system = reviewSystem
	if lang != "" && lang != "auto" {
		system += "\nAnswer in " + lang + "."
	} else {
		system += "\nAnswer in the language the data is written in."
	}
	return system, b.String()
}

// streamLLM runs the prompt through the suite's shared provider layer
// (MISSIONCTL_PROVIDER override, then ANTHROPIC/OPENAI/GEMINI keys, else
// local Ollama) and streams the answer to w.
func streamLLM(ctx context.Context, w io.Writer, system, user string) error {
	info, err := ai.Detect("MISSIONCTL")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, dashMutedStyle.Render("via "+info.Display))
	_, err = ai.Call(ctx, info, system, user, func(chunk string) { fmt.Fprint(w, chunk) })
	fmt.Fprintln(w)
	return err
}

var planLang, reviewLang string
var planShowPrompt, reviewShowPrompt bool

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "AI day plan from your calendar, open tasks and habits (prints, changes nothing)",
	RunE: func(_ *cobra.Command, _ []string) error {
		sys, user := buildPlanPrompt(gatherPlanContext(time.Now()), planLang)
		if planShowPrompt {
			fmt.Fprintln(cliOut, sys+"\n\n"+user)
			return nil
		}
		return streamLLM(context.Background(), cliOut, sys, user)
	},
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI weekly review across time, tasks, habits, budget and diary",
	RunE: func(_ *cobra.Command, _ []string) error {
		sys, user := buildReviewPrompt(gatherReviewContext(time.Now()), reviewLang)
		if reviewShowPrompt {
			fmt.Fprintln(cliOut, sys+"\n\n"+user)
			return nil
		}
		return streamLLM(context.Background(), cliOut, sys, user)
	},
}

func init() {
	planCmd.Flags().StringVar(&planLang, "lang", "auto", "Answer language (auto = language of your data)")
	planCmd.Flags().BoolVar(&planShowPrompt, "show-prompt", false, "Print the prompt instead of calling the AI (see exactly what would be sent)")
	reviewCmd.Flags().StringVar(&reviewLang, "lang", "auto", "Answer language (auto = language of your data)")
	reviewCmd.Flags().BoolVar(&reviewShowPrompt, "show-prompt", false, "Print the prompt instead of calling the AI")
	rootCmd.AddCommand(planCmd, reviewCmd)
}
