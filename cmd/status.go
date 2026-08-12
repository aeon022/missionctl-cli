package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

// runToolJSON shells out to a sibling tool's own CLI and decodes its JSON
// output into v. Every status function used to read that tool's SQLite
// database directly with hand-rolled SQL — several of those paths and
// column names had drifted from the tools' actual current schemas (found
// while fixing this: taskctl and calctl's DB paths were wrong, diaryctl's
// query referenced a "streaks" table and an "entry_date" column that don't
// exist), so the cards were silently showing "not configured" or 0 for
// tools that actually had data. Going through each tool's own --json
// output instead means the data layer can't drift out of sync with a
// schema change the way raw cross-repo SQL can. Returns false (not an
// error) if the tool isn't installed or the call fails, so a card
// degrades to "not configured" instead of erroring the whole dashboard.
func runToolJSON(bin string, args []string, v any) bool {
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		return false
	}
	return json.Unmarshal(out, v) == nil
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Daily briefing from all tool databases",
	RunE:  runStatus,
}

func expandHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func openDB(path string) (*sql.DB, error) {
	expanded := expandHome(path)
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		return nil, nil // not configured
	}
	db, err := sql.Open("sqlite", expanded)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// syncAge reports how long ago a tool's database file was last written —
// its DB file's mtime updates on every sync, so this is the same signal
// `missionctl doctor` already shows (toolDB/formatAge, defined in
// doctor.go), reused here instead of duplicated so a card's data can't be
// old enough to be misleading without it being visible right on the card.
func syncAge(tool string) string {
	if _, ok := toolDB[tool]; !ok {
		return ""
	}
	info, err := os.Stat(expandHome(resolvedToolDBPath(tool)))
	if err != nil {
		return "not synced yet"
	}
	return formatAge(time.Since(info.ModTime())) + " ago"
}

func runStatus(_ *cobra.Command, _ []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true)
	labelStyle := lipgloss.NewStyle().Width(12)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	now := time.Now()
	dateStr := now.Format("Mon Jan 02")

	// printStatus splits a status value on its optional detail line(s) and
	// indents continuations under the value column (not the label column),
	// so a card's extra detail line — added so the same data reads well in
	// both this plain view and the dashboard's boxes — doesn't run on
	// looking like part of the summary or drift under the wrong column.
	printStatus := func(label, tool string, s cardStatus) {
		lines := strings.Split(s.text, "\n")
		if age := syncAge(tool); age != "" {
			lines = append(lines, "synced "+age)
		}
		fmt.Printf("  %s %s\n", labelStyle.Render(label), lines[0])
		for _, l := range lines[1:] {
			fmt.Printf("  %s %s\n", labelStyle.Render(""), detailStyle.Render(l))
		}
	}

	fmt.Println()
	fmt.Printf("  %s\n", headerStyle.Render(fmt.Sprintf("missionctl status — %s", dateStr)))
	fmt.Println()

	printStatus("Tasks", "taskctl", taskStatus())
	printStatus("Calendar", "calctl", calStatus(now))
	printStatus("Timer", "timectl", timerStatus(now))
	printStatus("Diary", "diaryctl", diaryStatus())
	printStatus("Budget", "budgetctl", budgetStatus(now))
	printStatus("Habits", "habctl", habitStatus(now))
	printStatus("Notes", "notectl", noteStatus(now))
	printStatus("Mail", "mailctl", mailStatus())

	fmt.Println()
	return nil
}

func taskStatus() cardStatus {
	var resp struct {
		Overdue  int `json:"overdue"`
		DueToday int `json:"due_today"`
		Data     []struct {
			Title   string     `json:"title"`
			DueDate *time.Time `json:"due_date"`
		} `json:"data"`
	}
	if !runToolJSON("taskctl", []string{"today", "--json"}, &resp) {
		return cardStatus{text: "–  not configured"}
	}
	if resp.Overdue == 0 && resp.DueToday == 0 {
		return cardStatus{text: "nothing due today"}
	}
	var parts []string
	if resp.DueToday > 0 {
		parts = append(parts, fmt.Sprintf("%d due today", resp.DueToday))
	}
	if resp.Overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d overdue", resp.Overdue))
	}
	summary := strings.Join(parts, " · ")
	if len(resp.Data) > 0 {
		summary += "\n" + resp.Data[0].Title
	}
	urgency := urgencyNormal
	switch {
	case resp.Overdue > 0:
		urgency = urgencyCritical
	case resp.DueToday > 0:
		urgency = urgencyWarn
	}
	return cardStatus{text: summary, urgency: urgency}
}

func calStatus(now time.Time) cardStatus {
	var resp struct {
		Count int `json:"count"`
		Data  []struct {
			Title     string    `json:"title"`
			StartTime time.Time `json:"start_time"`
		} `json:"data"`
	}
	if !runToolJSON("calctl", []string{"list", "--today", "--format", "json"}, &resp) {
		return cardStatus{text: "–  not configured"}
	}
	if resp.Count == 0 {
		return cardStatus{text: "no events today"}
	}
	summary := fmt.Sprintf("%d events today", resp.Count)
	for _, e := range resp.Data {
		if e.StartTime.After(now) {
			return cardStatus{text: fmt.Sprintf("%s\nnext: %s at %s", summary, e.Title, e.StartTime.Format("15:04"))}
		}
	}
	return cardStatus{text: summary}
}

func timerStatus(_ time.Time) cardStatus {
	var resp struct {
		TotalHuman string `json:"total_human"`
		Entries    []struct {
			Task    string `json:"task"`
			Project string `json:"project"`
			Running bool   `json:"running"`
		} `json:"entries"`
	}
	if !runToolJSON("timectl", []string{"today", "--json"}, &resp) {
		return cardStatus{text: "–  not configured"}
	}
	todayLine := fmt.Sprintf("%s today", resp.TotalHuman)
	for _, e := range resp.Entries {
		if e.Running {
			task := e.Task
			if e.Project != "" {
				task = fmt.Sprintf("%s (%s)", e.Task, e.Project)
			}
			return cardStatus{text: "running: " + task + "\n" + todayLine}
		}
	}
	return cardStatus{text: "no timer running\n" + todayLine}
}

func diaryStatus() cardStatus {
	db, err := openDB(resolvedToolDBPath("diaryctl"))
	if err != nil || db == nil {
		return cardStatus{text: "–  not configured"}
	}
	defer db.Close()

	// Last entry date. There is no streaks table in diaryctl's schema — an
	// earlier version of this query referenced one, plus an "entry_date"
	// column that doesn't exist either (the real column is "date"); both
	// silently failed via a swallowed Scan error and always reported
	// "streak: 0 days", regardless of actual data.
	var lastDate sql.NullString
	_ = db.QueryRow(`SELECT date FROM entries ORDER BY date DESC LIMIT 1`).Scan(&lastDate)

	if !lastDate.Valid || lastDate.String == "" {
		return cardStatus{text: "no entries\nwrite your first one", urgency: urgencyWarn}
	}
	t, err := time.Parse("2006-01-02", lastDate.String[:10])
	if err != nil {
		return cardStatus{text: "no entries"}
	}
	today := time.Now().Truncate(24 * time.Hour)
	diff := today.Sub(t.Truncate(24 * time.Hour))
	switch {
	case diff < 24*time.Hour:
		return cardStatus{text: "last entry: today\nyou're up to date"}
	case diff < 48*time.Hour:
		return cardStatus{text: "last entry: yesterday\nno entry yet today", urgency: urgencyWarn}
	default:
		return cardStatus{text: fmt.Sprintf("last entry: %s\nno entry yet today", t.Format("Jan 02")), urgency: urgencyWarn}
	}
}

func budgetStatus(_ time.Time) cardStatus {
	var goals struct {
		Alerts int `json:"alerts"`
		Data   []struct {
			Category string  `json:"Category"`
			Percent  float64 `json:"Percent"`
		} `json:"data"`
	}
	if runToolJSON("budgetctl", []string{"goal", "list", "--json"}, &goals) && len(goals.Data) > 0 {
		worst := goals.Data[0]
		for _, g := range goals.Data {
			if g.Percent > worst.Percent {
				worst = g
			}
		}
		if goals.Alerts > 0 {
			text := fmt.Sprintf("%d goal(s) over/near budget\n%s at %.0f%%", goals.Alerts, worst.Category, worst.Percent)
			urgency := urgencyWarn
			if worst.Percent >= 100 {
				urgency = urgencyCritical
			}
			return cardStatus{text: text, urgency: urgency}
		}
		return cardStatus{text: fmt.Sprintf("%d goals on track\nhighest: %s at %.0f%%", len(goals.Data), worst.Category, worst.Percent)}
	}

	var sum struct {
		Expenses   float64            `json:"Expenses"`
		ByCategory map[string]float64 `json:"ByCategory"`
	}
	if !runToolJSON("budgetctl", []string{"summary", "--json"}, &sum) {
		return cardStatus{text: "–  not configured"}
	}
	summary := fmt.Sprintf("€%.0f spent this month", math.Abs(sum.Expenses))
	topCat, topAmt, found := "", 0.0, false
	for cat, amt := range sum.ByCategory {
		if a := math.Abs(amt); a > topAmt || !found {
			topCat, topAmt, found = cat, a, true
		}
	}
	if found {
		if topCat == "" {
			topCat = "(uncategorized)"
		}
		return cardStatus{text: fmt.Sprintf("%s\ntop: %s (€%.0f)", summary, topCat, topAmt)}
	}
	return cardStatus{text: summary}
}

func habitStatus(_ time.Time) cardStatus {
	var resp struct {
		Done  int `json:"done"`
		Total int `json:"total"`
		Data  []struct {
			Name         string `json:"name"`
			CheckedToday bool   `json:"checked_today"`
		} `json:"data"`
	}
	if !runToolJSON("habctl", []string{"today", "--json"}, &resp) {
		return cardStatus{text: "–  not configured"}
	}
	if resp.Total == 0 {
		return cardStatus{text: "no habits tracked"}
	}
	summary := fmt.Sprintf("%d/%d done today", resp.Done, resp.Total)
	if resp.Done == resp.Total {
		return cardStatus{text: summary + "\nall done! 🎉"}
	}
	for _, h := range resp.Data {
		if !h.CheckedToday {
			return cardStatus{text: summary + "\nnext: " + h.Name}
		}
	}
	return cardStatus{text: summary}
}

func noteStatus(now time.Time) cardStatus {
	db, err := openDB(resolvedToolDBPath("notectl"))
	if err != nil || db == nil {
		return cardStatus{text: "–  not configured"}
	}
	defer db.Close()

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&total); err != nil {
		return cardStatus{text: "–  not configured"}
	}

	today := now.Format("2006-01-02")
	var createdToday int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notes WHERE created LIKE ?`, today+"%").Scan(&createdToday)

	summary := fmt.Sprintf("%d notes", total)
	if createdToday > 0 {
		summary = fmt.Sprintf("%s · %d created today", summary, createdToday)
	}

	var lastTitle sql.NullString
	_ = db.QueryRow(`SELECT title FROM notes ORDER BY mod_time DESC LIMIT 1`).Scan(&lastTitle)
	if lastTitle.Valid && lastTitle.String != "" {
		return cardStatus{text: summary + "\nlatest: " + lastTitle.String}
	}
	return cardStatus{text: summary}
}

func mailStatus() cardStatus {
	db, err := openDB(resolvedToolDBPath("mailctl"))
	if err != nil || db == nil {
		return cardStatus{text: "–  not configured"}
	}
	defer db.Close()

	var unread int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE read = 0`).Scan(&unread); err != nil {
		return cardStatus{text: "–  not configured"}
	}
	if unread == 0 {
		return cardStatus{text: "0 unread\ninbox clear"}
	}

	var subject sql.NullString
	_ = db.QueryRow(`SELECT subject FROM messages WHERE read = 0 ORDER BY date DESC LIMIT 1`).Scan(&subject)
	summary := fmt.Sprintf("%d unread", unread)
	if subject.Valid && subject.String != "" {
		return cardStatus{text: summary + "\n" + subject.String}
	}
	return cardStatus{text: summary}
}
