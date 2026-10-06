package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/activity"
	"github.com/spf13/cobra"
)

// parseSince turns "today", "yesterday", "7d", "2w" or "YYYY-MM-DD" into the
// start of the window (local midnight), relative to now.
func parseSince(s string, now time.Time) (time.Time, error) {
	start, _ := activity.Day(now)
	switch s = strings.ToLower(strings.TrimSpace(s)); {
	case s == "" || s == "today":
		return start, nil
	case s == "yesterday":
		return start.AddDate(0, 0, -1), nil
	case strings.HasSuffix(s, "d") || strings.HasSuffix(s, "w"):
		var n int
		if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil || n < 1 {
			return time.Time{}, fmt.Errorf("bad --since %q (use e.g. 7d, 2w, today, yesterday, 2026-10-01)", s)
		}
		if s[len(s)-1] == 'w' {
			n *= 7
		}
		return start.AddDate(0, 0, -(n - 1)), nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("bad --since %q (use e.g. 7d, 2w, today, yesterday, 2026-10-01)", s)
	}
	return t, nil
}

// filterEvents keeps events of one tool ("" = all).
func filterEvents(evs []activity.Event, tool string) []activity.Event {
	if tool == "" {
		return evs
	}
	var out []activity.Event
	for _, e := range evs {
		if e.Tool == tool {
			out = append(out, e)
		}
	}
	return out
}

var (
	logSince   string
	logTool    string
	logJSON    bool
	logEnable  bool
	logDisable bool
	logDiary   string
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "What you did across the suite (activity log), grouped by day",
	Long: `Shows the suite's activity log: one line each time you added, completed, deleted,
checked, started, stopped, wrote, sent or published something in any tool. Only titles
are logged — never note bodies, mail text or amounts.

Settings (stored in ~/.config/missionctl/activity.yaml):
  --disable / --enable        stop or resume logging for every tool
  --diary ask|auto|off        what diaryctl does with the day's activity at day end
                              (ask = offer, auto = add automatically, off = never)
Setting MISSIONCTL_ACTIVITY=off in the environment disables logging too.`,
	Example: `  missionctl log
  missionctl log --since 7d --tool taskctl
  missionctl log --since yesterday --json
  missionctl log --diary auto`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case logDisable:
			return setAndReport(activity.SetEnabled(false), "Activity logging disabled.")
		case logEnable:
			return setAndReport(activity.SetEnabled(true), "Activity logging enabled.")
		case logDiary != "":
			m := activity.DiaryMode(strings.ToLower(logDiary))
			if m != activity.DiaryAsk && m != activity.DiaryAuto && m != activity.DiaryOff {
				return fmt.Errorf("--diary must be ask, auto or off")
			}
			return setAndReport(activity.SetDiaryMode(m), "diaryctl will "+map[activity.DiaryMode]string{
				activity.DiaryAsk:  "ask before adding the day's activity to your diary.",
				activity.DiaryAuto: "add the day's activity to your diary automatically.",
				activity.DiaryOff:  "never add activity to your diary.",
			}[m])
		}

		now := time.Now()
		from, err := parseSince(logSince, now)
		if err != nil {
			return err
		}
		evs, err := activity.Read(from, now.Add(time.Minute))
		if err != nil {
			return err
		}
		evs = filterEvents(evs, logTool)
		if logJSON {
			if evs == nil {
				evs = []activity.Event{}
			}
			b, _ := json.MarshalIndent(evs, "", "  ")
			fmt.Fprintln(cliOut, string(b))
			return nil
		}
		fmt.Fprint(cliOut, renderLog(evs, now))
		return nil
	},
}

func setAndReport(err error, msg string) error {
	if err != nil {
		return err
	}
	fmt.Fprintln(cliOut, "  "+msg)
	return nil
}

// renderLog groups events by day, newest day first, oldest event first within
// a day.
func renderLog(evs []activity.Event, now time.Time) string {
	if len(evs) == 0 {
		st := activity.Load()
		hint := "Nothing logged."
		if !st.Enabled {
			hint = "Activity logging is off (missionctl log --enable)."
		}
		return "  " + dashMutedStyle.Render(hint) + "\n"
	}
	var days []string
	byDay := map[string][]activity.Event{}
	for _, e := range evs {
		d := e.Time.Format("2006-01-02")
		if _, ok := byDay[d]; !ok {
			days = append(days, d)
		}
		byDay[d] = append(byDay[d], e)
	}
	var b strings.Builder
	for i := len(days) - 1; i >= 0; i-- {
		d := days[i]
		label := d
		switch d {
		case now.Format("2006-01-02"):
			label = "Today · " + d
		case now.AddDate(0, 0, -1).Format("2006-01-02"):
			label = "Yesterday · " + d
		}
		fmt.Fprintf(&b, "\n  %s\n", dashKeyStyle.Render(label))
		for _, e := range byDay[d] {
			fmt.Fprintf(&b, "    %s  %-9s %-10s %s\n", dashMutedStyle.Render(e.Time.Format("15:04")), e.Tool, e.Action, e.Title)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func init() {
	logCmd.Flags().StringVar(&logSince, "since", "today", "Window start: today, yesterday, 7d, 2w or YYYY-MM-DD")
	logCmd.Flags().StringVar(&logTool, "tool", "", "Only this tool (e.g. taskctl)")
	logCmd.Flags().BoolVar(&logJSON, "json", false, "Output as JSON")
	logCmd.Flags().BoolVar(&logDisable, "disable", false, "Stop logging activity")
	logCmd.Flags().BoolVar(&logEnable, "enable", false, "Resume logging activity")
	logCmd.Flags().StringVar(&logDiary, "diary", "", "Set what diaryctl does with the day's activity: ask, auto or off")
	rootCmd.AddCommand(logCmd)
}
