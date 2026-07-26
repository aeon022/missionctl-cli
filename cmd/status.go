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

func runStatus(_ *cobra.Command, _ []string) error {
	headerStyle := lipgloss.NewStyle().Bold(true)
	labelStyle := lipgloss.NewStyle().Width(12)
	dashStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	now := time.Now()
	dateStr := now.Format("Mon Jan 02")

	fmt.Println()
	fmt.Printf("  %s\n", headerStyle.Render(fmt.Sprintf("missionctl status — %s", dateStr)))
	fmt.Println()

	// Tasks
	taskLine := taskStatus()
	fmt.Printf("  %s %s\n", labelStyle.Render("Tasks"), taskLine)

	// Calendar
	calLine := calStatus(now)
	fmt.Printf("  %s %s\n", labelStyle.Render("Calendar"), calLine)

	// Timer
	timerLine := timerStatus(now)
	fmt.Printf("  %s %s\n", labelStyle.Render("Timer"), timerLine)

	// Diary
	diaryLine := diaryStatus()
	fmt.Printf("  %s %s\n", labelStyle.Render("Diary"), diaryLine)

	// Budget
	budgetLine := budgetStatus(now)
	fmt.Printf("  %s %s\n", labelStyle.Render("Budget"), budgetLine)

	// Habits
	habitLine := habitStatus(now)
	fmt.Printf("  %s %s\n", labelStyle.Render("Habits"), habitLine)

	// Notes
	noteLine := noteStatus(now)
	fmt.Printf("  %s %s\n", labelStyle.Render("Notes"), noteLine)

	// Mail
	mailLine := mailStatus()
	fmt.Printf("  %s %s\n", labelStyle.Render("Mail"), mailLine)

	fmt.Println()

	_ = dashStyle
	return nil
}

func taskStatus() string {
	var resp struct {
		Overdue  int `json:"overdue"`
		DueToday int `json:"due_today"`
	}
	if !runToolJSON("taskctl", []string{"today", "--json"}, &resp) {
		return "–  not configured"
	}
	if resp.Overdue == 0 && resp.DueToday == 0 {
		return "nothing due today"
	}
	var parts []string
	if resp.DueToday > 0 {
		parts = append(parts, fmt.Sprintf("%d due today", resp.DueToday))
	}
	if resp.Overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d overdue", resp.Overdue))
	}
	return strings.Join(parts, " · ")
}

func calStatus(now time.Time) string {
	var resp struct {
		Count int `json:"count"`
		Data  []struct {
			Title     string    `json:"title"`
			StartTime time.Time `json:"start_time"`
		} `json:"data"`
	}
	if !runToolJSON("calctl", []string{"list", "--today", "--format", "json"}, &resp) {
		return "–  not configured"
	}
	if resp.Count == 0 {
		return "no events today"
	}
	for _, e := range resp.Data {
		if e.StartTime.After(now) {
			return fmt.Sprintf("%d events today · next: %s at %s", resp.Count, e.Title, e.StartTime.Format("15:04"))
		}
	}
	return fmt.Sprintf("%d events today", resp.Count)
}

func timerStatus(_ time.Time) string {
	var resp struct {
		TotalHuman string `json:"total_human"`
		Entries    []struct {
			Task    string `json:"task"`
			Running bool   `json:"running"`
		} `json:"entries"`
	}
	if !runToolJSON("timectl", []string{"today", "--json"}, &resp) {
		return "–  not configured"
	}
	for _, e := range resp.Entries {
		if e.Running {
			return fmt.Sprintf("running: %s · %s today", e.Task, resp.TotalHuman)
		}
	}
	return fmt.Sprintf("no timer running · %s today", resp.TotalHuman)
}

func diaryStatus() string {
	db, err := openDB("~/.local/share/diaryctl/diary.db")
	if err != nil || db == nil {
		return "–  not configured"
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
		return "no entries"
	}
	t, err := time.Parse("2006-01-02", lastDate.String[:10])
	if err != nil {
		return "no entries"
	}
	today := time.Now().Truncate(24 * time.Hour)
	diff := today.Sub(t.Truncate(24 * time.Hour))
	switch {
	case diff < 24*time.Hour:
		return "last entry: today"
	case diff < 48*time.Hour:
		return "last entry: yesterday"
	default:
		return fmt.Sprintf("last entry: %s", t.Format("Jan 02"))
	}
}

func budgetStatus(_ time.Time) string {
	var goals struct {
		Alerts int `json:"alerts"`
		Data   []struct {
			Category string `json:"Category"`
		} `json:"data"`
	}
	if runToolJSON("budgetctl", []string{"goal", "list", "--json"}, &goals) && len(goals.Data) > 0 {
		if goals.Alerts > 0 {
			return fmt.Sprintf("%d goal(s) over/near budget", goals.Alerts)
		}
		return fmt.Sprintf("%d goals on track", len(goals.Data))
	}

	var sum struct {
		Expenses float64 `json:"Expenses"`
	}
	if !runToolJSON("budgetctl", []string{"summary", "--json"}, &sum) {
		return "–  not configured"
	}
	return fmt.Sprintf("€%.0f spent this month", math.Abs(sum.Expenses))
}

func habitStatus(_ time.Time) string {
	var resp struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	}
	if !runToolJSON("habctl", []string{"today", "--json"}, &resp) {
		return "–  not configured"
	}
	if resp.Total == 0 {
		return "no habits tracked"
	}
	return fmt.Sprintf("%d/%d done today", resp.Done, resp.Total)
}

func noteStatus(now time.Time) string {
	db, err := openDB("~/.local/share/notectl/notes.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&total); err != nil {
		return "–  not configured"
	}

	today := now.Format("2006-01-02")
	var createdToday int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notes WHERE created LIKE ?`, today+"%").Scan(&createdToday)

	if createdToday > 0 {
		return fmt.Sprintf("%d notes · %d created today", total, createdToday)
	}
	return fmt.Sprintf("%d notes", total)
}

func mailStatus() string {
	db, err := openDB("~/Library/Application Support/mailctl/mailctl.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	var unread int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE read = 0`).Scan(&unread); err != nil {
		return "–  not configured"
	}

	return fmt.Sprintf("%d unread", unread)
}
