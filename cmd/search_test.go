package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func fakeTools(t *testing.T, out map[string]string) {
	t.Helper()
	orig := runSearchTool
	runSearchTool = func(_ context.Context, bin string, _ []string) ([]byte, error) {
		if s, ok := out[bin]; ok {
			return []byte(s), nil
		}
		return nil, fmt.Errorf("%s: not installed", bin)
	}
	t.Cleanup(func() { runSearchTool = orig })
}

func TestSearchFiltersNonNativeAndTrustsNative(t *testing.T) {
	fakeTools(t, map[string]string{
		// non-native: bare list, must be filtered by the query (case-insensitive)
		"taskctl": `[{"title":"Steuer abgeben","list":"Arbeit","notes":"","due_date":"2026-10-12T00:00:00Z"},
		             {"title":"Milch kaufen","list":"Einkauf","notes":"Bio"}]`,
		// native: the tool already searched; items needn't contain the query text
		"notectl": `[{"title":"Meeting-Protokoll","path":"a.md"}]`,
		// envelope shape {"command","data":[...]}
		"habctl": `{"command":"today","data":[{"name":"Steuer-Habit","streak":3},{"name":"Sport"}]}`,
	})
	hits := searchAll(context.Background(), "steuer", 5, time.Now())

	var got []string
	for _, h := range hits {
		got = append(got, h.Tool+":"+h.Title)
	}
	want := []string{"taskctl:Steuer abgeben", "notectl:Meeting-Protokoll", "habctl:Steuer-Habit"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("hits = %v\nwant   %v", got, want)
	}
	if hits[0].When != "2026-10-12" {
		t.Errorf("When = %q, want date part only", hits[0].When)
	}
}

func TestSearchSnippetFromNonTitleField(t *testing.T) {
	fakeTools(t, map[string]string{
		"taskctl": `[{"title":"Anruf","notes":"bitte wegen der Steuererklärung nachfragen"}]`,
	})
	hits := searchAll(context.Background(), "steuer", 5, time.Now())
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, "Steuererklärung") {
		t.Fatalf("hits = %+v, want a snippet from notes", hits)
	}
}

func TestSearchSkipsMissingAndBrokenToolsAndHonorsLimit(t *testing.T) {
	fakeTools(t, map[string]string{
		"taskctl": `[{"title":"a x"},{"title":"b x"},{"title":"c x"}]`,
		"calctl":  `not json at all`,
	})
	hits := searchAll(context.Background(), "x", 2, time.Now())
	if len(hits) != 2 {
		t.Errorf("want 2 (limit per tool; broken/missing tools skipped), got %+v", hits)
	}
}

func TestSearchKeepsSourceOrderAndEmptyQueryMatchesAllNonNative(t *testing.T) {
	fakeTools(t, map[string]string{
		"habctl":  `[{"name":"H"}]`,
		"taskctl": `[{"title":"T"}]`,
	})
	hits := searchAll(context.Background(), "", 5, time.Now())
	if len(hits) != 2 || hits[0].Tool != "taskctl" || hits[1].Tool != "habctl" {
		t.Errorf("order/empty query: %+v", hits)
	}
}

func TestFindItemsShapes(t *testing.T) {
	if got := findItems(map[string]any{"count": 2.0, "tasks": []any{map[string]any{"title": "x"}}}); len(got) != 1 {
		t.Errorf("object holding an array: %v", got)
	}
	if got := findItems("nope"); got != nil {
		t.Errorf("scalar: %v", got)
	}
}
