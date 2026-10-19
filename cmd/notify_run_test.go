package cmd

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// notifyHarness resets the notify flags and replaces the banner so no macOS notification pops up.
func notifyHarness(t *testing.T) (posted *[]notification) {
	t.Helper()
	notifyDry, notifyInstall, notifyUninstall = false, false, false
	t.Cleanup(func() { notifyDry, notifyInstall, notifyUninstall = false, false, false })
	var got []notification
	orig := postBanner
	postBanner = func(n notification) error { got = append(got, n); return nil }
	t.Cleanup(func() { postBanner = orig })
	return &got
}

func TestGatherNotifyContextReadsHabitsTasksAndDoses(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{
		"habctl": {{args: "today --json", out: `{"data":[
			{"name":"Read","checked_today":false,"streak":5},
			{"name":"Walk","checked_today":true,"streak":9},
			{"name":"New habit","checked_today":false,"streak":0}]}`}},
		"taskctl":   {{args: "list --today --json", out: `{"data":[{"title":"a"},{"title":"b"}]}`}},
		"healthctl": {{args: "today --json", out: `{"Due":3}`}},
	})
	c := gatherNotifyContext(time.Date(2026, 10, 6, 19, 0, 0, 0, time.Local))
	if len(c.Habits) != 1 || c.Habits[0] != (habitRisk{"Read", 5}) {
		t.Errorf("only an unchecked habit with a streak is at risk: %+v", c.Habits)
	}
	if c.DueCount != 2 || c.DosesDue != 3 {
		t.Errorf("due=%d doses=%d", c.DueCount, c.DosesDue)
	}
}

func TestGatherNotifyContextWithoutOptionalTools(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	c := gatherNotifyContext(time.Now())
	if len(c.Habits) != 0 || c.DueCount != 0 || c.DosesDue != 0 {
		t.Errorf("missing tools must give an empty context, not an error: %+v", c)
	}
}

func TestNotifyPostsEachBannerOnceAndKeepsStatePrivate(t *testing.T) {
	posted := notifyHarness(t)
	fakeSuite(t, map[string][]fakeRule{"healthctl": {{args: "today --json", out: `{"Due":2}`}}})
	captureOut(t)

	for run := 1; run <= 2; run++ {
		if err := notifyCmd.RunE(nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(*posted) != 1 {
			t.Fatalf("run %d: %d banners posted, want exactly 1 in total: %+v", run, len(*posted), *posted)
		}
	}
	if b := (*posted)[0]; b.Title != "Medication" || b.Body != "medication due" || strings.Contains(b.Body, "2") {
		t.Errorf("health banners carry no names or counts: %+v", b)
	}
	info, err := os.Stat(notifyStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the state file holds event titles: mode %v, want 0600", info.Mode().Perm())
	}
}

func TestNotifyDryRunPostsAndRemembersNothing(t *testing.T) {
	posted := notifyHarness(t)
	fakeSuite(t, map[string][]fakeRule{"healthctl": {{args: "today --json", out: `{"Due":1}`}}})
	out := captureOut(t)
	notifyDry = true
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(*posted) != 0 || !strings.Contains(out.String(), "Medication") {
		t.Errorf("posted=%v out=%q", *posted, out.String())
	}
	if _, err := os.Stat(notifyStatePath()); err == nil {
		t.Error("a dry run must not write the state file")
	}
}

func TestNotifyRetriesABannerThatFailedToPost(t *testing.T) {
	notifyHarness(t)
	fakeSuite(t, map[string][]fakeRule{"healthctl": {{args: "today --json", out: `{"Due":1}`}}})
	captureOut(t)
	fail := true
	var attempts int
	postBanner = func(notification) error {
		attempts++
		if fail {
			return fmt.Errorf("osascript: not allowed")
		}
		return nil
	}
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
	fail = false
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Errorf("a banner that failed to post must not be marked as seen: %d attempts", attempts)
	}
}

func TestNotifyPlistIsValidXMLEvenForOddPaths(t *testing.T) {
	self := `/Users/a&b/<bin>/missionctl`
	var plist struct {
		Array []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal([]byte(notifyPlist(self, 300, "/Users/a&b/Logs")), &plist); err != nil {
		t.Fatalf("launchd would reject this plist: %v", err)
	}
	if len(plist.Array) != 2 || plist.Array[0] != self || plist.Array[1] != "notify" {
		t.Errorf("ProgramArguments = %q", plist.Array)
	}
}

func TestNotifyInstallAndUninstall(t *testing.T) {
	notifyHarness(t)
	calls := fakeSuite(t, map[string][]fakeRule{})
	out := captureOut(t)

	notifyInstall = true
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(notifyAgentPath())
	if err != nil || !strings.Contains(string(body), "<string>notify</string>") || !strings.Contains(string(body), "<integer>300</integer>") {
		t.Fatalf("plist: %v\n%s", err, body)
	}
	if !strings.Contains(out.String(), "launchctl load") {
		t.Errorf("the user is told how to activate it:\n%s", out)
	}
	if len(calls()) != 0 {
		t.Errorf("--install only writes the file, it must not run launchctl: %v", calls())
	}

	notifyInstall, notifyUninstall = false, true
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(notifyAgentPath()); err == nil {
		t.Error("uninstall removes the plist")
	}
	if err := notifyCmd.RunE(nil, nil); err != nil {
		t.Errorf("uninstalling twice is not an error: %v", err)
	}
}
