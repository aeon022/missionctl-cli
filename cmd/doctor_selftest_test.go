package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeInstalled puts an executable called name on PATH so exec.LookPath finds
// it; the doctor run itself is stubbed via runToolDoctor.
func fakeInstalled(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func stubDoctor(t *testing.T, out string, err error) {
	t.Helper()
	orig := runToolDoctor
	runToolDoctor = func(context.Context, string) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { runToolDoctor = orig })
}

func TestToolDoctorVerdict(t *testing.T) {
	fakeInstalled(t, "faketool")

	stubDoctor(t, "✓ Database  /x (3 rows)\n\nAll checks passed.\n", nil)
	if inst, ok, why := toolDoctorVerdict("faketool"); !inst || !ok || why != "" {
		t.Errorf("clean run: inst=%v ok=%v why=%q", inst, ok, why)
	}

	stubDoctor(t, "✓ Database  /x\n✗ Reminders.app   not found\n", fmt.Errorf("exit status 1"))
	if _, ok, why := toolDoctorVerdict("faketool"); ok || why != "✗ Reminders.app   not found" {
		t.Errorf("failing check: ok=%v why=%q (want the ✗ line)", ok, why)
	}

	// a ✗ line fails the tool even if the exit code is 0
	stubDoctor(t, "✗ Config   missing\n", nil)
	if _, ok, _ := toolDoctorVerdict("faketool"); ok {
		t.Error("✗ line with exit 0 must still fail")
	}

	// non-zero exit without a ✗ line: the first output line is the reason
	stubDoctor(t, "panic: boom\n", fmt.Errorf("exit status 2"))
	if _, ok, why := toolDoctorVerdict("faketool"); ok || why != "panic: boom" {
		t.Errorf("crash: ok=%v why=%q", ok, why)
	}

	if inst, _, _ := toolDoctorVerdict("definitely-not-installed-xyz"); inst {
		t.Error("a tool that isn't on PATH is 'not installed', not a failure")
	}
}

func TestSQLiteQuickCheck(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.db")
	db, err := sql.Open("sqlite", good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT); INSERT INTO t (v) VALUES ('a'),('b')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if got := sqliteQuickCheck(good); got != "ok" {
		t.Errorf("healthy db = %q, want ok", got)
	}

	bad := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(bad, []byte("this is definitely not a sqlite database, just text padding padding padding padding padding padding padding padding padding"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sqliteQuickCheck(bad); got == "ok" {
		t.Error("a garbage file must not pass the integrity check")
	}
}
