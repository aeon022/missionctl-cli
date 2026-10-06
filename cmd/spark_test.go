package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestSparklineScalingZeroAndSingle(t *testing.T) {
	if got := sparkline([]float64{0, 1, 2, 4, 8}); got != "▁▂▃▅█" {
		t.Errorf("scaled = %q, want ▁▂▃▅█", got)
	}
	if got := sparkline([]float64{0, 0, 0}); got != "▁▁▁" {
		t.Errorf("all zero = %q, want the lowest bar everywhere", got)
	}
	if got := sparkline([]float64{5}); got != "█" {
		t.Errorf("single value = %q, want a full bar", got)
	}
	// a tiny positive value must stay distinguishable from zero
	if got := sparkline([]float64{0, 0.01, 100}); got != "▁▂█" {
		t.Errorf("tiny value = %q, want ▁▂█", got)
	}
	if got := sparkline(nil); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestDailyBucketsLocalDaysAndWindow(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	pts := []datedValue{
		{time.Date(2026, 10, 6, 0, 5, 0, 0, time.Local), 10},  // today, just after midnight
		{time.Date(2026, 10, 6, 23, 55, 0, 0, time.Local), 5}, // today, late
		{time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local), 3},  // yesterday
		{time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local), 7},  // 6 days ago = first bucket
		{time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local), 99}, // 7 days ago: outside the window
		{time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local), 99}, // future: outside
	}
	got := dailyBuckets(now, 7, pts)
	want := []float64{7, 0, 0, 0, 0, 3, 15}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buckets = %v, want %v", got, want)
		}
	}
}

func TestWithSparkKeepsLineWidthAndSparkFlushRight(t *testing.T) {
	spark := []float64{1, 2, 3, 4, 5, 6, 7}
	line := withSpark("a rather long detail line that must be cut", spark, 30)
	if w := len([]rune(ansi.Strip(line))); w != 30 {
		t.Errorf("width = %d, want exactly 30: %q", w, line)
	}
	if !strings.HasSuffix(line, sparkline(spark)) {
		t.Errorf("sparkline not flush right: %q", line)
	}
	if got := withSpark("detail", nil, 30); got != "detail" {
		t.Errorf("no spark must leave the line alone, got %q", got)
	}
	if got := withSpark("", spark, 30); !strings.HasSuffix(got, sparkline(spark)) {
		t.Errorf("empty detail line still shows the sparkline: %q", got)
	}
}
