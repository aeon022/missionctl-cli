package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	_ "modernc.org/sqlite"
	"github.com/spf13/cobra"
)

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
	db, err := openDB("~/.local/share/taskctl/tasks.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	var open int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='needsAction'`).Scan(&open); err != nil {
		return "–  not configured"
	}

	today := time.Now().Format("2006-01-02")
	var dueToday int
	// due date may be stored as ISO string; attempt both date-only and datetime prefix
	row := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='needsAction' AND (due = ? OR due LIKE ?)`, today, today+"%")
	_ = row.Scan(&dueToday)

	return fmt.Sprintf("%d open · %d due today", open, dueToday)
}

func calStatus(now time.Time) string {
	db, err := openDB("~/.local/share/calctl/cal.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	today := now.Format("2006-01-02")

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM events WHERE start LIKE ?`, today+"%",
	).Scan(&count); err != nil {
		return "–  not configured"
	}

	// Find the next event from now
	nowStr := now.Format("2006-01-02T15:04")
	var nextSummary, nextStart string
	row := db.QueryRow(
		`SELECT summary, start FROM events WHERE start >= ? ORDER BY start ASC LIMIT 1`, nowStr,
	)
	if err := row.Scan(&nextSummary, &nextStart); err == nil && nextSummary != "" {
		// Parse time for display
		t, perr := time.Parse("2006-01-02T15:04:05Z07:00", nextStart)
		if perr != nil {
			t, perr = time.Parse("2006-01-02T15:04:05", nextStart)
		}
		if perr == nil {
			return fmt.Sprintf("%d events today · next: %s at %s", count, nextSummary, t.Format("15:04"))
		}
		return fmt.Sprintf("%d events today · next: %s", count, nextSummary)
	}

	return fmt.Sprintf("%d events today", count)
}

func timerStatus(now time.Time) string {
	db, err := openDB("~/.local/share/timectl/time.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	// Check for running timer (end IS NULL)
	var runningDesc sql.NullString
	_ = db.QueryRow(`SELECT description FROM entries WHERE end IS NULL ORDER BY start DESC LIMIT 1`).Scan(&runningDesc)

	today := now.Format("2006-01-02")

	// Sum of completed entries today (in seconds)
	var totalSecs sql.NullFloat64
	_ = db.QueryRow(
		`SELECT SUM((julianday(end) - julianday(start)) * 86400)
		 FROM entries
		 WHERE start LIKE ? AND end IS NOT NULL`, today+"%",
	).Scan(&totalSecs)

	hours := 0
	mins := 0
	if totalSecs.Valid && totalSecs.Float64 > 0 {
		secs := int(totalSecs.Float64)
		hours = secs / 3600
		mins = (secs % 3600) / 60
	}

	todayStr := fmt.Sprintf("%dh %02dm today", hours, mins)

	if runningDesc.Valid && runningDesc.String != "" {
		return fmt.Sprintf("running: %s · %s", runningDesc.String, todayStr)
	}
	return fmt.Sprintf("no timer running · %s", todayStr)
}

func diaryStatus() string {
	db, err := openDB("~/.local/share/diaryctl/diary.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	// Try to get streak from streaks table
	var streak sql.NullInt64
	_ = db.QueryRow(`SELECT current_streak FROM streaks ORDER BY rowid DESC LIMIT 1`).Scan(&streak)

	// Last entry date
	var lastDate sql.NullString
	_ = db.QueryRow(`SELECT entry_date FROM entries ORDER BY entry_date DESC LIMIT 1`).Scan(&lastDate)

	streakStr := "streak: 0 days"
	if streak.Valid {
		streakStr = fmt.Sprintf("streak: %d days", streak.Int64)
	}

	lastStr := "no entries"
	if lastDate.Valid && lastDate.String != "" {
		t, err := time.Parse("2006-01-02", lastDate.String[:10])
		if err == nil {
			today := time.Now().Truncate(24 * time.Hour)
			diff := today.Sub(t.Truncate(24 * time.Hour))
			switch {
			case diff < 24*time.Hour:
				lastStr = "last entry: today"
			case diff < 48*time.Hour:
				lastStr = "last entry: yesterday"
			default:
				lastStr = fmt.Sprintf("last entry: %s", t.Format("Jan 02"))
			}
		}
	}

	return fmt.Sprintf("%s · %s", streakStr, lastStr)
}

func budgetStatus(now time.Time) string {
	db, err := openDB("~/.local/share/budgetctl/budget.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	month := now.Format("2006-01")

	var total sql.NullFloat64
	_ = db.QueryRow(
		`SELECT SUM(amount) FROM transactions WHERE date LIKE ? AND amount > 0`, month+"%",
	).Scan(&total)

	if !total.Valid || total.Float64 == 0 {
		return "€0 spent this month"
	}

	return fmt.Sprintf("€%.0f spent this month", total.Float64)
}

func habitStatus(now time.Time) string {
	db, err := openDB("~/.local/share/habctl/habits.db")
	if err != nil || db == nil {
		return "–  not configured"
	}
	defer db.Close()

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM habits WHERE archived = 0`).Scan(&total); err != nil {
		return "–  not configured"
	}

	today := now.Format("2006-01-02")
	var doneToday int
	_ = db.QueryRow(
		`SELECT COUNT(DISTINCT habit_id) FROM checkins WHERE date = ?`, today,
	).Scan(&doneToday)

	if total == 0 {
		return "no habits tracked"
	}
	return fmt.Sprintf("%d/%d done today", doneToday, total)
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
