package cmd

import (
	"charm.land/lipgloss/v2"
	"database/sql"
	"fmt"
	"github.com/aeon022/missionctl-core/theme"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// postctl keeps one SQLite DB per profile; the dashboard reads them directly
// (read-only) and merges all profiles, prefixing named ones on the card.

type postctlSource struct{ profile, path string }

// postctlDataDir reads `data_dir:` from a postctl config.yaml ("" = unset).
func postctlDataDir(cfg string) string {
	b, err := os.ReadFile(cfg)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "data_dir:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func postctlSources() []postctlSource {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	db := func(cfg, fallbackDir string) string {
		dir := fallbackDir
		if d := postctlDataDir(cfg); d != "" {
			dir = expandHome(d)
		}
		return filepath.Join(dir, "postctl.db")
	}
	base := filepath.Join(home, ".local", "share", "postctl")
	var out []postctlSource
	if p := db(filepath.Join(home, ".config", "postctl", "config.yaml"), base); fileExists(p) {
		out = append(out, postctlSource{"", p})
	}
	entries, _ := os.ReadDir(filepath.Join(base, "profiles"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfg := filepath.Join(home, ".config", "postctl", "profiles", e.Name(), "config.yaml")
		if p := db(cfg, filepath.Join(base, "profiles", e.Name())); fileExists(p) {
			out = append(out, postctlSource{e.Name(), p})
		}
	}
	return out
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

type postctlPost struct {
	profile, platform, title string
	at                       time.Time
}

type postctlData struct {
	upcoming          []postctlPost // scheduled and not yet due, soonest first
	scheduled, drafts int
	failed, overdue   int
	lastPosted        time.Time
}

func loadPostctl() (postctlData, bool) {
	var d postctlData
	srcs := postctlSources()
	if len(srcs) == 0 {
		return d, false
	}
	now := time.Now()
	for _, s := range srcs {
		db, err := sql.Open("sqlite", "file:"+s.path+"?mode=ro")
		if err != nil {
			continue
		}
		rows, err := db.Query(`SELECT platform, COALESCE(title,''), status, COALESCE(scheduled_at,''), COALESCE(posted_at,'') FROM posts WHERE status IN ('scheduled','draft') OR posted_at != '' OR (status='failed' AND updated_at >= datetime('now','-14 days'))`)
		if err != nil {
			db.Close()
			continue
		}
		for rows.Next() {
			var plat, title, status, sched, posted string
			if rows.Scan(&plat, &title, &status, &sched, &posted) != nil {
				continue
			}
			switch status {
			case "draft":
				d.drafts++
			case "failed":
				d.failed++
			case "scheduled":
				t, err := time.Parse(time.RFC3339, sched)
				if err != nil {
					continue
				}
				d.scheduled++
				if t.Before(now) {
					d.overdue++
				} else {
					d.upcoming = append(d.upcoming, postctlPost{s.profile, plat, title, t})
				}
			}
			if t, err := time.Parse(time.RFC3339, posted); err == nil && t.After(d.lastPosted) {
				d.lastPosted = t
			}
		}
		rows.Close()
		db.Close()
	}
	sort.Slice(d.upcoming, func(i, j int) bool { return d.upcoming[i].at.Before(d.upcoming[j].at) })
	return d, true
}

var platformShort = map[string]string{"instagram": "IG", "twitter": "X", "threads": "Threads", "facebook": "FB", "linkedin": "LI", "bluesky": "Bsky", "mastodon": "Masto"}

func postctlWhen(t, now time.Time) string {
	switch {
	case t.Format("2006-01-02") == now.Format("2006-01-02"):
		return "today " + t.Format("15:04")
	case t.Format("2006-01-02") == now.AddDate(0, 0, 1).Format("2006-01-02"):
		return "tomorrow " + t.Format("15:04")
	}
	return t.Format("Mon 02.01. 15:04")
}

func postctlLine(p postctlPost, now time.Time) string {
	plat := platformShort[p.platform]
	if plat == "" {
		plat = p.platform
	}
	s := postctlWhen(p.at, now) + " · " + plat
	if p.profile != "" {
		s += " (" + p.profile + ")"
	}
	return s
}

// postctlStatus feeds the wide postctl card: left column = what goes out next,
// right column = queue counts and health. text/urgency drive the usual
// border colour and the not-configured state.
func postctlStatus(now time.Time) cardStatus {
	d, ok := loadPostctl()
	if !ok {
		return cardStatus{text: "–  not configured"}
	}
	st := cardStatus{text: fmt.Sprintf("%d scheduled", d.scheduled)}
	for _, p := range d.upcoming[:min(len(d.upcoming), 12)] {
		st.items = append(st.items, postctlLine(p, now)+" – "+p.title)
	}
	for _, p := range d.upcoming[:min(len(d.upcoming), 3)] {
		st.left = append(st.left, postctlLine(p, now))
	}
	if len(st.left) == 0 {
		st.left = []string{"nothing scheduled"}
	}
	st.right = []string{fmt.Sprintf("%d scheduled · %d drafts", d.scheduled, d.drafts)}
	last := "never posted"
	if !d.lastPosted.IsZero() {
		last = "last post " + ago(now.Sub(d.lastPosted))
	}
	switch {
	case d.failed > 0:
		st.urgency = urgencyCritical
		st.right = append(st.right, fmt.Sprintf("✗ %d failed", d.failed), last)
	case d.overdue > 0:
		st.urgency = urgencyWarn
		st.right = append(st.right, fmt.Sprintf("⚠ %d overdue – scheduler running?", d.overdue), last)
	default:
		st.right = append(st.right, "✓ all clear", last)
	}
	return st
}

// renderWideCard is the postctl card: a full-row banner with a filled title
// bar so it stands out from the plain per-tool cards.
func (m dashboardModel) renderWideCard(i int) string {
	c := dashboardCards[i]
	w := m.cardWidthOf(i)
	inner := w - 4
	selected := i == m.cursor
	v := m.values[i]

	border := lipgloss.RoundedBorder()
	switch {
	case selected:
		border = lipgloss.ThickBorder()
	case i == m.hover:
		border = lipgloss.DoubleBorder()
	}
	bc := cardBorderColor(v.urgency, selected)
	if v.urgency == urgencyNormal && !selected {
		bc = c.color // accent border even when idle: this card is the showpiece
	}

	bar := lipgloss.NewStyle().Bold(true).Foreground(theme.OnAccent).Background(c.color).Render(" " + c.icon + " POSTCTL ")
	sub := lipgloss.NewStyle().Foreground(dashSubtle).Render("  social publishing")
	head := bar + sub
	if c.key != "" {
		badge := lipgloss.NewStyle().Foreground(dashSubtle).Render("[" + c.key + "]")
		head += strings.Repeat(" ", max(inner-lipgloss.Width(head)-lipgloss.Width(badge), 1)) + badge
	}
	rule := lipgloss.NewStyle().Foreground(dashSubtle).Render(strings.Repeat("─", inner))

	if !m.loaded[i] || len(v.left) == 0 && len(v.right) == 0 {
		msg := v.text
		if !m.loaded[i] {
			msg = "– loading…"
		}
		return lipgloss.NewStyle().Border(border).BorderForeground(bc).Padding(0, 1).Width(w).
			Render(head + "\n" + rule + "\n" + lipgloss.NewStyle().Foreground(dashSubtle).Render(truncate(msg, inner)))
	}

	lw := inner * 3 / 5
	rw := inner - lw - 2
	label := lipgloss.NewStyle().Bold(true).Foreground(c.color)
	col := func(title string, lines []string, width int) string {
		out := label.Render(title)
		for _, l := range lines {
			out += "\n" + lipgloss.NewStyle().Width(width).Render(truncate(l, width))
		}
		return out
	}
	right := make([]string, len(v.right))
	copy(right, v.right)
	if len(right) > 1 {
		switch v.urgency {
		case urgencyCritical:
			right[1] = dashErrStyle.Render(truncate(right[1], rw))
		case urgencyWarn:
			right[1] = lipgloss.NewStyle().Foreground(dashWarnColor).Render(truncate(right[1], rw))
		default:
			right[1] = dashOKStyle.Render(truncate(right[1], rw))
		}
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, col("NEXT UP", v.left, lw), "  ", col("QUEUE", right, rw))
	return lipgloss.NewStyle().Border(border).BorderForeground(bc).Padding(0, 1).Width(w).
		Render(head + "\n" + rule + "\n" + body)
}
