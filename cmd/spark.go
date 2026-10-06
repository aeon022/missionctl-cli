package cmd

import (
	"charm.land/lipgloss/v2"
	"math"
	"strings"
	"time"
)

var sparkBars = []rune("▁▂▃▄▅▆▇█")

// sparkline renders vals as one bar per value, scaled to the largest. Zero
// (or an all-zero series) is the lowest bar; any positive value is at least
// the second bar so "a little" never looks like "nothing".
func sparkline(vals []float64) string {
	top := 0.0
	for _, v := range vals {
		top = math.Max(top, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if top > 0 && v > 0 {
			i = max(int(math.Round(v/top*float64(len(sparkBars)-1))), 1)
		}
		b.WriteRune(sparkBars[i])
	}
	return b.String()
}

type datedValue struct {
	t time.Time
	v float64
}

// dailyBuckets sums points into `days` local-calendar-day buckets ending today
// (index days-1 is today); points outside the window are ignored.
func dailyBuckets(now time.Time, days int, pts []datedValue) []float64 {
	midnight := func(t time.Time) time.Time {
		y, m, d := t.In(now.Location()).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	}
	first := midnight(now).AddDate(0, 0, -(days - 1))
	out := make([]float64, days)
	for _, p := range pts {
		i := int(math.Round(midnight(p.t).Sub(first).Hours() / 24))
		if i >= 0 && i < days {
			out[i] += p.v
		}
	}
	return out
}

// withSpark puts the sparkline flush right on a card's detail line (width w),
// truncating the detail text to make room. No sparkline: the line is unchanged.
func withSpark(detail string, spark []float64, w int) string {
	if len(spark) == 0 {
		return detail
	}
	sp := sparkline(spark)
	room := w - len(spark) - 1
	if room < 1 {
		return detail
	}
	text := truncate(detail, room)
	pad := room - lipgloss.Width(text)
	return text + strings.Repeat(" ", max(pad, 0)) + " " + sp
}
