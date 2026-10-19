package cmd

import (
	"strings"
	"testing"
)

func bundleStubs() map[string][]fakeRule {
	ok := []fakeRule{{args: "license activate KEY-123", out: "✓ License activated!"}}
	return map[string][]fakeRule{"calctl": ok, "budgetctl": ok, "notectl": ok, "habctl": ok, "mailctl": ok, "postctl": ok}
}

func TestActivateAllTouchesOnlyBundleToolsAndPassesTheKey(t *testing.T) {
	calls := fakeSuite(t, bundleStubs())
	for _, r := range activateAll("KEY-123") {
		if !r.installed || !r.ok {
			t.Errorf("%s should be activated: %+v", r.tool, r)
		}
	}
	got := strings.Join(calls(), "\n")
	for _, tool := range []string{"calctl", "budgetctl", "notectl", "habctl", "mailctl"} {
		if !strings.Contains(got, tool+" <license> <activate> <KEY-123>") {
			t.Errorf("%s was not called with the key:\n%s", tool, got)
		}
	}
	// postctl is its own product: a Bundle key must never be run against it (it would overwrite its real key with "invalid")
	if strings.Contains(got, "postctl") {
		t.Errorf("postctl must not be activated with a Bundle key:\n%s", got)
	}
}

func TestActivateAllReportsFailuresAndMissingTools(t *testing.T) {
	stubs := bundleStubs()
	stubs["budgetctl"] = []fakeRule{{args: "license activate KEY-123", out: "checking…\n✗ Activation failed: invalid key", code: 1}}
	delete(stubs, "habctl") // not installed
	fakeSuite(t, stubs)
	byTool := map[string]toolResult{}
	for _, r := range activateAll("KEY-123") {
		byTool[r.tool] = r
	}
	if r := byTool["budgetctl"]; !r.installed || r.ok || r.detail != "✗ Activation failed: invalid key" {
		t.Errorf("budgetctl failure: %+v", r)
	}
	if r := byTool["habctl"]; r.installed || r.ok {
		t.Errorf("habctl is not installed: %+v", r)
	}
	if r := byTool["calctl"]; !r.ok {
		t.Errorf("a failing tool must not stop the others: %+v", r)
	}
}

func TestRunLicenseActivateOutcomes(t *testing.T) {
	t.Run("success trims the key and never prints it", func(t *testing.T) {
		calls := fakeSuite(t, bundleStubs())
		out := captureOut(t)
		if err := runLicenseActivate(nil, []string{"  KEY-123 \n"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Bundle unlocked on 5 tool(s)") {
			t.Errorf("output: %q", out.String())
		}
		if strings.Contains(out.String(), "KEY-123") {
			t.Errorf("the key must not be echoed:\n%s", out.String())
		}
		if len(calls()) != 5 {
			t.Errorf("expected 5 tool calls, got %v", calls())
		}
	})
	t.Run("a failing tool turns into an error", func(t *testing.T) {
		stubs := bundleStubs()
		stubs["mailctl"] = []fakeRule{{args: "license activate KEY-123", code: 1}}
		fakeSuite(t, stubs)
		out := captureOut(t)
		err := runLicenseActivate(nil, []string{"KEY-123"})
		if err == nil || !strings.Contains(err.Error(), "1 of 5 tool(s) failed") {
			t.Errorf("err = %v", err)
		}
		if !strings.Contains(out.String(), "mailctl") {
			t.Errorf("the failing tool is named:\n%s", out.String())
		}
	})
	t.Run("nothing installed", func(t *testing.T) {
		fakeSuite(t, map[string][]fakeRule{})
		captureOut(t)
		err := runLicenseActivate(nil, []string{"KEY-123"})
		if err == nil || !strings.Contains(err.Error(), "no licensable tools are installed") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestStatusAllIncludesPostctlAndParsesTheTypeLine(t *testing.T) {
	status := "License Type: PRO\nLicense Key:  A83-…8878\nStatus:       GRANTED (Verified with Polar.sh)"
	stubs := map[string][]fakeRule{
		"calctl":    {{args: "license status", out: status}},
		"postctl":   {{args: "license status", out: "License Type: CORE (MIT)\nStatus: No license registered."}},
		"budgetctl": {{args: "license status", out: "boom", code: 1}},
	}
	calls := fakeSuite(t, stubs)
	byTool := map[string]toolResult{}
	for _, r := range statusAll() {
		byTool[r.tool] = r
	}
	if r := byTool["calctl"]; !r.ok || r.detail != "License Type: PRO" {
		t.Errorf("calctl: %+v", r)
	}
	if r := byTool["postctl"]; !r.ok || r.detail != "License Type: CORE (MIT)" {
		t.Errorf("postctl status is read-only and must be reported: %+v", r)
	}
	if r := byTool["budgetctl"]; r.ok || !strings.HasPrefix(r.detail, "error: boom") {
		t.Errorf("a failing status call is reported as an error: %+v", r)
	}
	if r := byTool["habctl"]; r.installed || r.detail != "not installed" {
		t.Errorf("habctl: %+v", r)
	}
	for _, c := range calls() {
		if !strings.Contains(c, "<license> <status>") {
			t.Errorf("status must only run `license status`, got %q", c)
		}
	}
}

func TestRunLicenseStatusPrintsEveryTool(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"calctl": {{args: "license status", out: "License Type: PRO"}}})
	out := captureOut(t)
	if err := runLicenseStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range licensedTools {
		if !strings.Contains(out.String(), name) {
			t.Errorf("%s missing from:\n%s", name, out.String())
		}
	}
}

func TestResultLinePicksTheInformativeLine(t *testing.T) {
	for _, tc := range []struct{ out, prefix, want string }{
		{"a\n\nb\n", "", "b"},
		{"x\nLicense Type: PRO\nStatus: ok", "License Type:", "License Type: PRO"},
		{"x\ny\n", "License Type:", "y"},
		{"", "✗", ""},
		{"  ✗ failed  \nlast", "✗", "✗ failed"},
	} {
		if got := resultLine(tc.out, tc.prefix); got != tc.want {
			t.Errorf("resultLine(%q,%q) = %q, want %q", tc.out, tc.prefix, got, tc.want)
		}
	}
}
