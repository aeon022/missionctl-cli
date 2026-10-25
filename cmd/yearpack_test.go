package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYearpackRunsEveryExportForTheYearAndFlagsWhatIsOpen(t *testing.T) {
	calls := fakeSuite(t, map[string][]fakeRule{
		"invoicectl": {{args: "list --json", out: `[{"state":"overdue","open_cents":100,"currency":"EUR"},{"state":"draft","open_cents":0,"currency":"EUR"}]`}},
		"receiptctl": {{args: "list --json", out: `{"data":[{"status":"ok","reviewed":false,"txn_ref":""}]}`}},
		"budgetctl":  {},
	})
	dir := filepath.Join(t.TempDir(), "pack")
	var b bytes.Buffer
	if err := runYearpack(&b, dir, 2026); err != nil {
		t.Fatalf("%v\n%s", err, b.String())
	}
	got := strings.Join(calls(), "\n")
	for _, want := range []string{
		"invoicectl <export> <--from> <2026-01-01> <--to> <2026-12-31> <--out> <" + filepath.Join(dir, "invoices") + ">",
		"receiptctl <export> <--from> <2026-01-01> <--to> <2026-12-31> <--out> <" + filepath.Join(dir, "receipts") + ">",
		"budgetctl <export> <--year> <2026> <--output> <" + filepath.Join(dir, "bookings.csv") + ">",
		"<--summary>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing call %q in:\n%s", want, got)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "README.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Year pack 2026", "not tax advice", "receipts: 1 unchecked, 1 without a booking", "invoices: 1 open, 1 overdue, 1 draft"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("README lacks %q:\n%s", want, raw)
		}
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("the pack holds financial records: folder mode %v", fi.Mode().Perm())
	}
	if err := runYearpack(&b, dir, 2026); err == nil {
		t.Error("a folder that already has content must be refused")
	}
}

func TestYearpackSkipsMissingToolsAndReportsFailures(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{"budgetctl": {{args: "export --year 2026 --output " + "X", code: 1}}})
	dir := filepath.Join(t.TempDir(), "pack")
	var b bytes.Buffer
	if err := runYearpack(&b, dir, 2026); err != nil {
		t.Fatalf("missing tools are skipped, not failed: %v\n%s", err, b.String())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "README.txt"))
	if !strings.Contains(string(raw), "invoicectl not installed, skipped") || !strings.Contains(string(raw), "nothing flagged") {
		t.Errorf("README:\n%s", raw)
	}
}
