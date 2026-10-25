package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// The plain review is the deterministic half of `missionctl review`: the same tools' --json output, summed up
// locally with no AI provider and no network. Sections of tools that are not installed are simply absent.
// Health appears as one aggregate percentage (never medication names); money only with --money.

type weekSummary struct {
	From     string           `json:"from"`
	To       string           `json:"to"`
	Days     int              `json:"days"`
	Tasks    *taskWeek        `json:"tasks,omitempty"`
	Time     *timeWeek        `json:"time,omitempty"`
	Habits   *habitWeek       `json:"habits,omitempty"`
	Health   *healthWeek      `json:"health,omitempty"`
	Money    *moneyWeek       `json:"money,omitempty"`
	Receipts *receiptOverview `json:"receipts,omitempty"`
	Invoices *invoiceOverview `json:"invoices,omitempty"`
}

type taskWeek struct {
	Done    int      `json:"done"`
	Overdue int      `json:"overdue"`
	Titles  []string `json:"titles,omitempty"` // most recent first, at most 5
}

type timeShare struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
}

type timeWeek struct {
	TotalSeconds int64       `json:"total_seconds"`
	Shares       []timeShare `json:"shares,omitempty"`
}

type habitLine struct {
	Name      string `json:"name"`
	DaysDone  int    `json:"days_done"`
	DaysTotal int    `json:"days_total"`
	Streak    int    `json:"streak"`
}

type habitWeek struct {
	Habits     []habitLine `json:"habits"`
	BestStreak int         `json:"best_streak"`
}

type healthWeek struct {
	Pct    float64 `json:"pct"`
	Judged int     `json:"judged"`
	Missed int     `json:"missed"`
}

type moneyWeek struct {
	Income   float64          `json:"income"`
	Expenses float64          `json:"expenses"` // positive number
	Top      []categoryAmount `json:"top,omitempty"`
	Wealth   string           `json:"wealth,omitempty"`
}

type categoryAmount struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

type receiptOverview struct {
	Total     int `json:"total"`
	Unclear   int `json:"unclear"`
	Unchecked int `json:"unchecked"`
	Unmatched int `json:"unmatched"`
}

type invoiceOverview struct {
	Open         int   `json:"open"`
	Overdue      int   `json:"overdue"`
	Drafts       int   `json:"drafts"`
	OpenCents    int64 `json:"open_cents"`    // EUR only; other currencies are counted, not summed
	OverdueCents int64 `json:"overdue_cents"` // EUR only
}

func onPath(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

func gatherWeek(now time.Time, days int, money bool) weekSummary {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := today.AddDate(0, 0, -(days - 1))
	w := weekSummary{From: from.Format("2006-01-02"), To: today.Format("2006-01-02"), Days: days}
	w.Tasks = gatherTasks(from, today)
	w.Time = gatherTime(days)
	w.Habits = gatherHabits(days)
	w.Health = gatherHealth(days)
	w.Receipts = gatherReceipts()
	w.Invoices = gatherInvoices()
	if w.Invoices != nil && !money { // amounts only when asked for, like the rest of the money data
		w.Invoices.OpenCents, w.Invoices.OverdueCents = 0, 0
	}
	if money {
		w.Money = gatherMoney(from)
	}
	return w
}

func gatherTasks(from, today time.Time) *taskWeek {
	if !onPath("taskctl") {
		return nil
	}
	var r struct {
		Data []struct {
			Title       string     `json:"title"`
			Status      string     `json:"status"`
			DueDate     *time.Time `json:"due_date"`
			CompletedAt *time.Time `json:"completed_at"`
		} `json:"data"`
	}
	if !runToolJSON("taskctl", []string{"list", "--all", "--json"}, &r) {
		return nil
	}
	t := &taskWeek{}
	type done struct {
		title string
		at    time.Time
	}
	var ds []done
	for _, k := range r.Data {
		switch {
		case k.Status == "completed" && k.CompletedAt != nil && !k.CompletedAt.Before(from):
			ds = append(ds, done{k.Title, *k.CompletedAt})
		case k.Status != "completed" && k.DueDate != nil && k.DueDate.Before(today):
			t.Overdue++
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].at.After(ds[j].at) })
	t.Done = len(ds)
	for i := 0; i < len(ds) && i < 5; i++ {
		t.Titles = append(t.Titles, ds[i].title)
	}
	return t
}

func gatherTime(days int) *timeWeek {
	if !onPath("timectl") {
		return nil
	}
	var r struct {
		Entries []struct {
			Task    string `json:"task"`
			Project string `json:"project"`
			Seconds int64  `json:"duration_seconds"`
		} `json:"entries"`
		Total int64 `json:"total_seconds"`
	}
	if !runToolJSON("timectl", []string{"log", "--json", "-d", fmt.Sprint(days)}, &r) {
		return nil
	}
	by := map[string]int64{}
	for _, e := range r.Entries {
		name := e.Project
		if name == "" {
			name = e.Task
		}
		if name == "" {
			name = "(no project)"
		}
		by[name] += e.Seconds
	}
	t := &timeWeek{TotalSeconds: r.Total}
	for n, s := range by {
		t.Shares = append(t.Shares, timeShare{n, s})
	}
	sort.Slice(t.Shares, func(i, j int) bool {
		if t.Shares[i].Seconds != t.Shares[j].Seconds {
			return t.Shares[i].Seconds > t.Shares[j].Seconds
		}
		return t.Shares[i].Name < t.Shares[j].Name
	})
	return t
}

func gatherHabits(days int) *habitWeek {
	if !onPath("habctl") {
		return nil
	}
	var r struct {
		Data []habitLine `json:"data"`
	}
	if !runToolJSON("habctl", []string{"stats", "--json", "--days", fmt.Sprint(days)}, &r) || len(r.Data) == 0 {
		return nil
	}
	h := &habitWeek{Habits: r.Data}
	for _, l := range r.Data {
		if l.Streak > h.BestStreak {
			h.BestStreak = l.Streak
		}
	}
	return h
}

func gatherHealth(days int) *healthWeek {
	if !onPath("healthctl") {
		return nil
	}
	var r struct {
		Pct     float64 `json:"pct"`
		Judged  int     `json:"judged"`
		Missed  int     `json:"missed"`
		HasData bool    `json:"has_data"`
	}
	if !runToolJSON("healthctl", []string{"adherence", "--json", "--days", fmt.Sprint(days)}, &r) || !r.HasData {
		return nil
	}
	return &healthWeek{Pct: r.Pct, Judged: r.Judged, Missed: r.Missed}
}

func gatherReceipts() *receiptOverview {
	if !onPath("receiptctl") {
		return nil
	}
	var r struct {
		Data []struct {
			Status   string `json:"status"`
			Reviewed bool   `json:"reviewed"`
			TxnRef   string `json:"txn_ref"`
		} `json:"data"`
	}
	if !runToolJSON("receiptctl", []string{"list", "--json"}, &r) || len(r.Data) == 0 {
		return nil
	}
	o := &receiptOverview{Total: len(r.Data)}
	for _, x := range r.Data {
		if x.Status != "ok" {
			o.Unclear++
		}
		if !x.Reviewed {
			o.Unchecked++
		}
		if x.TxnRef == "" {
			o.Unmatched++
		}
	}
	return o
}

func gatherInvoices() *invoiceOverview {
	if !onPath("invoicectl") {
		return nil
	}
	var rows []struct {
		State     string `json:"state"`
		OpenCents int64  `json:"open_cents"`
		Currency  string `json:"currency"`
	}
	if !runToolJSON("invoicectl", []string{"list", "--json"}, &rows) {
		return nil
	}
	o := &invoiceOverview{}
	for _, r := range rows {
		if r.State == "draft" {
			o.Drafts++
		}
		if r.OpenCents > 0 {
			o.Open++
			if r.Currency == "EUR" {
				o.OpenCents += r.OpenCents
			}
		}
		if r.State == "overdue" {
			o.Overdue++
			if r.Currency == "EUR" {
				o.OverdueCents += r.OpenCents
			}
		}
	}
	if o.Open == 0 && o.Drafts == 0 {
		return nil
	}
	return o
}

func gatherMoney(from time.Time) *moneyWeek {
	if !onPath("budgetctl") {
		return nil
	}
	var rows []struct {
		Date     time.Time
		Amount   float64
		Category string
	}
	if !runToolJSON("budgetctl", []string{"list", "--json", "-n", "100000"}, &rows) {
		return nil
	}
	m := &moneyWeek{}
	byCat := map[string]float64{}
	for _, r := range rows {
		if r.Date.Before(from) {
			continue
		}
		if r.Amount >= 0 {
			m.Income += r.Amount
			continue
		}
		m.Expenses += -r.Amount
		c := r.Category
		if c == "" {
			c = "(uncategorized)"
		}
		byCat[c] += -r.Amount
	}
	for c, a := range byCat {
		m.Top = append(m.Top, categoryAmount{c, a})
	}
	sort.Slice(m.Top, func(i, j int) bool {
		if m.Top[i].Amount != m.Top[j].Amount {
			return m.Top[i].Amount > m.Top[j].Amount
		}
		return m.Top[i].Category < m.Top[j].Category
	})
	if len(m.Top) > 3 {
		m.Top = m.Top[:3]
	}
	if w := wealthStatus(); w.text != "" {
		m.Wealth = strings.ReplaceAll(strings.SplitN(w.text, "\n", 2)[0], "\n", " ")
	}
	return m
}

// euro shows cents below 100 so a 4,50 coffee is not rounded away; whole euros above.
func euro(f float64) string {
	if f < 100 {
		return fmt.Sprintf("€%.2f", f)
	}
	return fmt.Sprintf("€%.0f", f)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func hm(sec int64) string {
	h, m := sec/3600, sec%3600/60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %02dm", h, m)
}

func renderWeek(w io.Writer, s weekSummary) {
	row := func(label, text string) { fmt.Fprintf(w, "  %-10s %s\n", label, text) }
	cont := func(text string) { fmt.Fprintf(w, "  %-10s %s\n", "", text) }
	fmt.Fprintf(w, "\n  missionctl review — last %s (%s – %s)\n\n", plural(s.Days, "day", "days"), s.From, s.To)
	any := false
	if t := s.Tasks; t != nil {
		any = true
		line := fmt.Sprintf("%d done", t.Done)
		if t.Overdue > 0 {
			line += fmt.Sprintf(" · %d overdue", t.Overdue)
		}
		row("Tasks", line)
		if len(t.Titles) > 0 {
			more := ""
			if t.Done > len(t.Titles) {
				more = fmt.Sprintf(" (+%d)", t.Done-len(t.Titles))
			}
			cont(strings.Join(t.Titles, ", ") + more)
		}
	}
	if t := s.Time; t != nil {
		any = true
		row("Time", hm(t.TotalSeconds))
		var parts []string
		for i, sh := range t.Shares {
			if i == 4 {
				parts = append(parts, fmt.Sprintf("+%d more", len(t.Shares)-4))
				break
			}
			parts = append(parts, sh.Name+" "+hm(sh.Seconds))
		}
		if len(parts) > 0 {
			cont(strings.Join(parts, " · "))
		}
	}
	if h := s.Habits; h != nil {
		any = true
		var parts []string
		for _, l := range h.Habits {
			parts = append(parts, fmt.Sprintf("%s %d/%d", l.Name, l.DaysDone, l.DaysTotal))
		}
		row("Habits", strings.Join(parts, " · "))
		if h.BestStreak > 0 {
			cont("best streak " + plural(h.BestStreak, "day", "days"))
		}
	}
	if h := s.Health; h != nil {
		any = true
		row("Health", fmt.Sprintf("%.0f%% of doses taken (%d missed)", h.Pct, h.Missed))
	}
	if m := s.Money; m != nil {
		any = true
		row("Money", fmt.Sprintf("spent %s · income %s", euro(m.Expenses), euro(m.Income)))
		var parts []string
		for _, c := range m.Top {
			parts = append(parts, c.Category+" "+euro(c.Amount))
		}
		if len(parts) > 0 {
			cont("top: " + strings.Join(parts, ", "))
		}
		if m.Wealth != "" {
			cont(m.Wealth)
		}
	}
	if r := s.Receipts; r != nil {
		any = true
		row("Receipts", fmt.Sprintf("%d to check · %d not matched to a booking (of %d)", r.Unchecked, r.Unmatched, r.Total))
	}
	if i := s.Invoices; i != nil {
		any = true
		line := fmt.Sprintf("%d open", i.Open)
		if i.OpenCents > 0 {
			line += " (" + euro(float64(i.OpenCents)/100) + ")"
		}
		if i.Overdue > 0 {
			line += fmt.Sprintf(" · %d overdue", i.Overdue)
			if i.OverdueCents > 0 {
				line += " (" + euro(float64(i.OverdueCents)/100) + ")"
			}
		}
		if i.Drafts > 0 {
			line += " · " + plural(i.Drafts, "draft", "drafts")
		}
		row("Invoices", line)
	}
	if !any {
		fmt.Fprintln(w, "  no tool data available")
	}
	fmt.Fprintln(w)
}

func renderWeekJSON(w io.Writer, s weekSummary) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
