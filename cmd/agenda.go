package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var agendaCmd = &cobra.Command{
	Use:   "agenda",
	Short: "Today's calendar events, due tasks, and timer sessions in one timeline",
	RunE:  runAgenda,
}

// agendaItem is one entry in the merged timeline. hasTime distinguishes a
// real point in time (a calendar event's start, a timer session's start)
// from a task whose due date carries no specific time (the common case —
// Reminders due dates are usually just a date, midnight) — those go in
// their own "sometime today" bucket up top instead of being sorted in
// among timed events at a misleading 00:00.
type agendaItem struct {
	when    time.Time
	hasTime bool
	icon    string
	color   lipgloss.Color
	text    string
}

// buildAgendaItems fetches and dedups today's calendar events, due tasks,
// and timer sessions — shared by the standalone `agenda` command and the
// dashboard TUI's in-app agenda view so both show exactly the same data.
func buildAgendaItems(now time.Time) []agendaItem {
	var items []agendaItem

	var cal struct {
		Data []struct {
			Title     string    `json:"title"`
			StartTime time.Time `json:"start_time"`
		} `json:"data"`
	}
	if runToolJSON("calctl", []string{"list", "--today", "--format", "json"}, &cal) {
		for _, e := range cal.Data {
			items = append(items, agendaItem{when: e.StartTime, hasTime: true, icon: "📅", color: lipgloss.Color("42"), text: e.Title})
		}
	}

	var tasks struct {
		Data []struct {
			Title   string     `json:"title"`
			DueDate *time.Time `json:"due_date"`
		} `json:"data"`
	}
	if runToolJSON("taskctl", []string{"today", "--json"}, &tasks) {
		for _, t := range tasks.Data {
			if t.DueDate == nil {
				continue
			}
			label := t.Title
			today := t.DueDate.Format("2006-01-02") == now.Format("2006-01-02")
			if t.DueDate.Before(now) && !today {
				label += " (overdue)"
			}
			hasTime := t.DueDate.Hour() != 0 || t.DueDate.Minute() != 0
			items = append(items, agendaItem{when: *t.DueDate, hasTime: hasTime, icon: "✓", color: lipgloss.Color("39"), text: label})
		}
	}

	var timer struct {
		Entries []struct {
			Task      string `json:"task"`
			Project   string `json:"project"`
			StartedAt string `json:"started_at"`
			Running   bool   `json:"running"`
		} `json:"entries"`
	}
	if runToolJSON("timectl", []string{"today", "--json"}, &timer) {
		for _, e := range timer.Entries {
			t, err := time.Parse(time.RFC3339, e.StartedAt)
			if err != nil {
				continue
			}
			label := e.Task
			if e.Project != "" {
				label += " (" + e.Project + ")"
			}
			if e.Running {
				label += " — running"
			}
			items = append(items, agendaItem{when: t, hasTime: true, icon: "⏱", color: lipgloss.Color("221"), text: label})
		}
	}

	// calctl can carry two rows for the same real event: a local echo
	// created immediately by `calctl add`, plus the real Apple Calendar
	// event `calctl sync` fetches afterward — sync doesn't currently
	// retire the local echo once the real one is confirmed, so `calctl
	// list` itself (not just this agenda) can double-list a freshly added
	// event until it ages out. Dedup by (icon, title, time) as a cheap
	// client-side guard against that until calctl's sync is fixed to
	// reconcile the two itself.
	seen := make(map[string]bool, len(items))
	deduped := items[:0]
	for _, it := range items {
		key := fmt.Sprintf("%s|%s|%s", it.icon, it.text, it.when.Format(time.RFC3339))
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, it)
	}
	return deduped
}

// splitAgendaItems buckets items into time-less ("sometime today") and
// timed entries, the latter sorted chronologically.
func splitAgendaItems(items []agendaItem) (allDay, timed []agendaItem) {
	for _, it := range items {
		if it.hasTime {
			timed = append(timed, it)
		} else {
			allDay = append(allDay, it)
		}
	}
	sort.Slice(timed, func(i, j int) bool { return timed[i].when.Before(timed[j].when) })
	return
}

func runAgenda(_ *cobra.Command, _ []string) error {
	now := time.Now()
	allDay, timed := splitAgendaItems(buildAgendaItems(now))

	headerStyle := lipgloss.NewStyle().Bold(true)
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(6)

	fmt.Println()
	fmt.Printf("  %s\n", headerStyle.Render("Today's agenda — "+now.Format("Mon Jan 02")))
	fmt.Println()

	if len(timed) == 0 && len(allDay) == 0 {
		fmt.Println("  Nothing scheduled today.")
		fmt.Println()
		return nil
	}

	printItem := func(it agendaItem, timeLabel string) {
		iconStyle := lipgloss.NewStyle().Foreground(it.color)
		fmt.Printf("  %s %s  %s\n", timeStyle.Render(timeLabel), iconStyle.Render(it.icon), it.text)
	}

	for _, it := range allDay {
		printItem(it, "")
	}
	for _, it := range timed {
		printItem(it, it.when.Format("15:04"))
	}

	fmt.Println()
	return nil
}

func init() {
	rootCmd.AddCommand(agendaCmd)
}
