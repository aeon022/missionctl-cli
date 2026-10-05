package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coreconfig "github.com/aeon022/missionctl-core/config"
)

// filterCards returns the cards named in ids, in that order (ids are the
// lower-cased labels: tasks, calendar, timer, diary, budget, habits, notes,
// mail). Unknown ids are ignored; an empty/unusable list means "show all".
// Number-key badges are renumbered so 1..N always match what's on screen.
func filterCards(all []dashboardCard, ids []string) []dashboardCard {
	byID := map[string]dashboardCard{}
	for _, c := range all {
		byID[strings.ToLower(c.label)] = c
	}
	var out []dashboardCard
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if c, ok := byID[id]; ok && !seen[id] {
			out = append(out, c)
			seen[id] = true
		}
	}
	if len(out) == 0 {
		out = append(out, all...)
	}
	for i := range out {
		if i < 9 {
			out[i].key = strconv.Itoa(i + 1)
		} else {
			out[i].key = "" // only 1-9 are single-key jumps
		}
	}
	return out
}

// loadCardIDs reads `cards:` from <config dir>/missionctl/dashboard.yaml.
// A missing or unreadable file just means the default (all cards).
func loadCardIDs() []string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	s := coreconfig.NewStore("dashboard")
	s.AddPath(filepath.Join(dir, "missionctl"))
	if s.Read() != nil {
		return nil
	}
	list, _ := s.Get("cards").([]any)
	var ids []string
	for _, v := range list {
		if str, ok := v.(string); ok {
			ids = append(ids, str)
		}
	}
	return ids
}
