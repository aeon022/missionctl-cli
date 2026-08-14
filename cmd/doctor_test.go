package cmd

import "testing"

// Fixtures captured live from `launchctl list <label>` on this machine —
// see parseDaemonStatus's doc comment for why this is parsed as plain text
// rather than JSON.
const taskctlRunningFixture = `{
	"StandardOutPath" = "/Users/gweiher/Library/Logs/taskctl/taskctl-daemon.log";
	"LimitLoadToSessionType" = "Aqua";
	"StandardErrorPath" = "/Users/gweiher/Library/Logs/taskctl/taskctl-daemon.log";
	"Label" = "com.taskctl.daemon";
	"OnDemand" = false;
	"LastExitStatus" = 0;
	"PID" = 85615;
	"Program" = "/Users/gweiher/.local/bin/taskctl";
};`

const diaryctlIdleFixture = `{
	"StandardOutPath" = "/Users/gweiher/.local/share/diaryctl/logs/diaryctl.log";
	"LimitLoadToSessionType" = "Aqua";
	"StandardErrorPath" = "/Users/gweiher/.local/share/diaryctl/logs/diaryctl.error.log";
	"Label" = "sh.missionctl.diaryctl";
	"OnDemand" = true;
	"LastExitStatus" = 0;
	"Program" = "/Users/gweiher/.local/bin/diaryctl";
};`

// TestParseDaemonStatus_ContinuousDaemonRunning covers the case a plain
// "loaded" check couldn't distinguish from a crashed one: OnDemand=false
// (meant to run continuously) with a live PID and a clean last exit.
func TestParseDaemonStatus_ContinuousDaemonRunning(t *testing.T) {
	got := parseDaemonStatus(taskctlRunningFixture)
	if got.pid != "85615" {
		t.Errorf("pid = %q, want %q", got.pid, "85615")
	}
	if got.onDemand {
		t.Error("onDemand = true, want false (taskctl daemon is meant to run continuously)")
	}
	if got.lastExitStatus != 0 {
		t.Errorf("lastExitStatus = %d, want 0", got.lastExitStatus)
	}
}

// TestParseDaemonStatus_OnDemandDaemonIdle covers the normal, healthy state
// for a scheduled (not continuous) daemon between trigger windows: no PID
// at all (correct — it only runs at its trigger time, e.g. diaryctl's daily
// 17:30), which checkDaemons must not mistake for "should be running but
// isn't".
func TestParseDaemonStatus_OnDemandDaemonIdle(t *testing.T) {
	got := parseDaemonStatus(diaryctlIdleFixture)
	if got.pid != "" {
		t.Errorf("pid = %q, want \"\" (on-demand job idle between triggers)", got.pid)
	}
	if !got.onDemand {
		t.Error("onDemand = false, want true (diaryctl daemon only runs on its schedule)")
	}
	if got.lastExitStatus != 0 {
		t.Errorf("lastExitStatus = %d, want 0", got.lastExitStatus)
	}
}

// A daemon whose last run crashed must be reported regardless of OnDemand —
// this is the "silently looked identical to loaded" gap checkDaemons exists
// to close.
func TestParseDaemonStatus_NonZeroExitDetected(t *testing.T) {
	fixture := `{
	"Label" = "com.taskctl.daemon";
	"OnDemand" = false;
	"LastExitStatus" = 1;
};`
	got := parseDaemonStatus(fixture)
	if got.lastExitStatus != 1 {
		t.Errorf("lastExitStatus = %d, want 1", got.lastExitStatus)
	}
}

// Malformed/empty input must not panic — just report the zero value
// (no PID, no exit status, on-demand false), same as any other field a
// regex didn't find a match for.
func TestParseDaemonStatus_EmptyInput(t *testing.T) {
	got := parseDaemonStatus("")
	if got.pid != "" || got.lastExitStatus != 0 || got.onDemand {
		t.Errorf("parseDaemonStatus(\"\") = %+v, want the zero value", got)
	}
}
