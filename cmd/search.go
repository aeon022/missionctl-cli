package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// searchSource describes how to ask one tool for searchable items. Tools with
// a native search (notectl, mailctl, budgetctl) get the query and their hits
// are trusted; for the rest we pull a bounded --json list and filter here, so
// every tool's own data_dir/profile logic is respected without reading its
// database directly.
type searchSource struct {
	tool   string
	icon   string
	native bool
	args   func(q string, now time.Time) []string
}

var searchSources = []searchSource{
	{"taskctl", "✓", false, func(string, time.Time) []string { return []string{"list", "--all", "--json"} }},
	{"calctl", "📅", false, func(_ string, now time.Time) []string {
		return []string{"list", "--format", "json",
			"--from", now.AddDate(0, 0, -30).Format("2006-01-02"), "--to", now.AddDate(0, 0, 90).Format("2006-01-02")}
	}},
	{"notectl", "📝", true, func(q string, _ time.Time) []string { return []string{"search", q, "--json", "-n", "30"} }},
	{"mailctl", "✉", true, func(q string, _ time.Time) []string { return []string{"search", q, "--json", "--count", "30"} }},
	{"budgetctl", "💰", true, func(q string, _ time.Time) []string { return []string{"list", "-q", q, "--json", "-n", "30"} }},
	{"diaryctl", "📔", false, func(string, time.Time) []string { return []string{"list", "--json", "--limit", "200"} }},
	{"timectl", "⏱", false, func(string, time.Time) []string { return []string{"log", "--json", "-d", "90"} }},
	{"habctl", "🔥", false, func(string, time.Time) []string { return []string{"today", "--json"} }},
}

// SearchHit is one match, in the tool-agnostic shape the CLI prints and the
// dashboard can jump from.
type SearchHit struct {
	Tool    string `json:"tool"`
	Icon    string `json:"icon"`
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
	When    string `json:"when,omitempty"`
}

var (
	titleKeys = []string{"title", "name", "task", "subject", "summary", "description", "text", "content"}
	whenKeys  = []string{"due_date", "start_time", "started_at", "date", "created_at", "updated_at", "modified", "timestamp"}
)

// runSearchTool runs bin with args under a timeout and returns its stdout.
// It is a variable so tests can substitute canned JSON for the real tools.
var runSearchTool = func(ctx context.Context, bin string, args []string) ([]byte, error) {
	return exec.CommandContext(ctx, bin, args...).Output()
}

// searchAll queries every source in parallel and returns the hits grouped in
// source order, at most perTool each. A tool that isn't installed, errors out
// or times out is simply skipped — search degrades, never fails.
func searchAll(ctx context.Context, query string, perTool int, now time.Time) []SearchHit {
	results := make([][]SearchHit, len(searchSources))
	var wg sync.WaitGroup
	for i, src := range searchSources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			out, err := runSearchTool(cctx, src.tool, src.args(query, now))
			if err != nil {
				return
			}
			results[i] = extractHits(src, out, query, perTool)
		}()
	}
	wg.Wait()
	var all []SearchHit
	for _, r := range results {
		all = append(all, r...)
	}
	return all
}

// extractHits turns a tool's JSON (a top-level array, or an object holding
// one — e.g. {"command":..., "data":[...]}) into hits. Non-native sources are
// filtered by a case-insensitive substring match over every string field.
func extractHits(src searchSource, raw []byte, query string, limit int) []SearchHit {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	items := findItems(v)
	q := strings.ToLower(strings.TrimSpace(query))
	var hits []SearchHit
	for _, it := range items {
		strs := stringFields(it)
		var snippet string
		if !src.native {
			hay, sn := matchIn(strs, q)
			if !hay {
				continue
			}
			snippet = sn
		}
		h := SearchHit{Tool: src.tool, Icon: src.icon, Snippet: snippet}
		for _, k := range titleKeys {
			if s, ok := strs[k]; ok && s != "" {
				h.Title = s
				break
			}
		}
		if h.Title == "" {
			continue
		}
		if h.Snippet == h.Title {
			h.Snippet = ""
		}
		for _, k := range whenKeys {
			if s, ok := strs[k]; ok && s != "" {
				h.When = shortWhen(s)
				break
			}
		}
		hits = append(hits, h)
		if limit > 0 && len(hits) >= limit {
			break
		}
	}
	return hits
}

// findItems returns the list of objects in v: v itself if it is an array,
// else the first array-of-objects value found in the top-level object.
func findItems(v any) []map[string]any {
	toMaps := func(a []any) []map[string]any {
		var out []map[string]any
		for _, e := range a {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	switch t := v.(type) {
	case []any:
		return toMaps(t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if a, ok := t[k].([]any); ok {
				if m := toMaps(a); len(m) > 0 {
					return m
				}
			}
		}
	}
	return nil
}

// stringFields flattens an item to its top-level string values (numbers and
// nested values are ignored — enough to match titles, notes, names).
func stringFields(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[strings.ToLower(k)] = s
		}
	}
	return out
}

// matchIn reports whether q occurs in any field and returns a snippet around
// the first match outside the title fields (the title is shown anyway).
func matchIn(strs map[string]string, q string) (bool, string) {
	if q == "" {
		return true, ""
	}
	keys := make([]string, 0, len(strs))
	for k := range strs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	found := false
	snippet := ""
	for _, k := range keys {
		s := strs[k]
		i := strings.Index(strings.ToLower(s), q)
		if i < 0 {
			continue
		}
		found = true
		isTitle := false
		for _, tk := range titleKeys {
			if tk == k {
				isTitle = true
			}
		}
		if !isTitle && snippet == "" {
			r := []rune(s)
			start := len([]rune(s[:i])) - 20
			if start < 0 {
				start = 0
			}
			end := min(start+70, len(r))
			snippet = strings.ReplaceAll(string(r[start:end]), "\n", " ")
		}
	}
	return found, snippet
}

func shortWhen(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return s
}

var (
	searchPerTool int
	searchJSON    bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tasks, events, notes, mail, budget, diary, time and habits at once",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		hits := searchAll(context.Background(), strings.Join(args, " "), searchPerTool, time.Now())
		if searchJSON {
			b, _ := json.MarshalIndent(hits, "", "  ")
			fmt.Fprintln(cliOut, string(b))
			return nil
		}
		if len(hits) == 0 {
			fmt.Fprintln(cliOut, dashMutedStyle.Render("  Nothing found."))
			return nil
		}
		last := ""
		for _, h := range hits {
			if h.Tool != last {
				fmt.Fprintf(cliOut, "\n  %s %s\n", h.Icon, dashKeyStyle.Render(h.Tool))
				last = h.Tool
			}
			line := "    " + h.Title
			if h.When != "" {
				line += dashMutedStyle.Render("  " + h.When)
			}
			if h.Snippet != "" {
				line += dashMutedStyle.Render("  …" + h.Snippet + "…")
			}
			fmt.Fprintln(cliOut, line)
		}
		fmt.Fprintln(cliOut)
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchPerTool, "per-tool", 5, "Max hits per tool")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(searchCmd)
}
