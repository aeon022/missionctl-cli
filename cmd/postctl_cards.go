package cmd

import (
	"database/sql"
	"fmt"
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

// postsStatus is the "Posts" card: what goes out next.
func postsStatus(now time.Time) cardStatus {
	d, ok := loadPostctl()
	if !ok {
		return cardStatus{text: "–  not configured"}
	}
	if len(d.upcoming) == 0 {
		return cardStatus{text: fmt.Sprintf("nothing scheduled\n%d drafts", d.drafts)}
	}
	var items []string
	for _, p := range d.upcoming[:min(len(d.upcoming), 12)] {
		items = append(items, postctlLine(p, now)+" – "+p.title)
	}
	return cardStatus{
		text:  "next " + postctlLine(d.upcoming[0], now) + fmt.Sprintf("\n%d scheduled · %d drafts", d.scheduled, d.drafts),
		items: items,
	}
}

// postHealthStatus is the "Publish" card: is anything stuck or failing.
func postHealthStatus(now time.Time) cardStatus {
	d, ok := loadPostctl()
	if !ok {
		return cardStatus{text: "–  not configured"}
	}
	last := "never posted"
	if !d.lastPosted.IsZero() {
		last = "last post " + ago(now.Sub(d.lastPosted))
	}
	switch {
	case d.failed > 0:
		return cardStatus{text: fmt.Sprintf("%d failed\n%s", d.failed, last), urgency: urgencyCritical}
	case d.overdue > 0:
		return cardStatus{text: fmt.Sprintf("%d overdue – scheduler running?\n%s", d.overdue, last), urgency: urgencyWarn}
	}
	return cardStatus{text: "all clear\n" + last}
}
