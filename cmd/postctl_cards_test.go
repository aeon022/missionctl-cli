package cmd

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const postsSchema = `CREATE TABLE posts (id TEXT, platform TEXT, title TEXT, status TEXT, scheduled_at TEXT, posted_at TEXT, updated_at TEXT)`

func mkPostctlDB(t *testing.T, path string, rows ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(postsSchema); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO posts VALUES ` + r); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
	}
}

func rfc(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }

func TestPostctlDataDirParsesTheConfig(t *testing.T) {
	dir := t.TempDir()
	for body, want := range map[string]string{
		"data_dir: ~/Dropbox/postctl\n": "~/Dropbox/postctl",
		"x: 1\ndata_dir: \"/srv/pc\"\n": "/srv/pc",
		"data_dir: '/a b'\n":            "/a b",
		"other: value\n":                "",
	} {
		p := filepath.Join(dir, "c.yaml")
		os.WriteFile(p, []byte(body), 0o600)
		if got := postctlDataDir(p); got != want {
			t.Errorf("%q → %q, want %q", body, got, want)
		}
	}
	if postctlDataDir(filepath.Join(dir, "missing.yaml")) != "" {
		t.Error("a missing config means unset")
	}
}

func TestPostctlSourcesFindDefaultAndProfileDatabases(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	home, _ := os.UserHomeDir()
	if len(postctlSources()) != 0 {
		t.Fatal("no databases yet")
	}
	mkPostctlDB(t, filepath.Join(home, ".local", "share", "postctl", "postctl.db"))
	mkPostctlDB(t, filepath.Join(home, ".local", "share", "postctl", "profiles", "bruecke", "postctl.db"))
	// a profile whose config points its data elsewhere
	custom := filepath.Join(home, "Sync", "work")
	mkPostctlDB(t, filepath.Join(custom, "postctl.db"))
	os.MkdirAll(filepath.Join(home, ".local", "share", "postctl", "profiles", "work"), 0o755)
	cfgDir := filepath.Join(home, ".config", "postctl", "profiles", "work")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("data_dir: "+custom+"\n"), 0o600)

	got := map[string]string{}
	for _, s := range postctlSources() {
		got[s.profile] = s.path
	}
	if len(got) != 3 || got[""] == "" || !strings.HasSuffix(got["bruecke"], "profiles/bruecke/postctl.db") || got["work"] != filepath.Join(custom, "postctl.db") {
		t.Errorf("sources = %v", got)
	}
}

func TestLoadPostctlCountsAndNeverWrites(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	home, _ := os.UserHomeDir()
	db := filepath.Join(home, ".local", "share", "postctl", "postctl.db")
	mkPostctlDB(t, db,
		fmt.Sprintf(`('1','instagram','Soon','scheduled','%s','','')`, rfc(2*time.Hour)),
		fmt.Sprintf(`('2','threads','Later','scheduled','%s','','')`, rfc(30*time.Hour)),
		fmt.Sprintf(`('3','x','Overdue','scheduled','%s','','')`, rfc(-time.Hour)),
		`('4','linkedin','A draft','draft','','','')`,
		fmt.Sprintf(`('5','facebook','Recent failure','failed','','','%s')`, time.Now().Add(-24*time.Hour).UTC().Format("2006-01-02 15:04:05")),
		fmt.Sprintf(`('6','facebook','Old failure','failed','','','%s')`, time.Now().Add(-30*24*time.Hour).UTC().Format("2006-01-02 15:04:05")),
		fmt.Sprintf(`('7','instagram','Posted','posted','','%s','')`, rfc(-3*time.Hour)),
	)
	before := sha256.Sum256(mustRead(t, db))
	d, ok := loadPostctl()
	if !ok {
		t.Fatal("a database exists")
	}
	if d.scheduled != 3 || d.overdue != 1 || d.drafts != 1 || d.failed != 1 || len(d.upcoming) != 2 {
		t.Errorf("counts: %+v", d)
	}
	if d.upcoming[0].title != "Soon" || d.upcoming[1].title != "Later" {
		t.Errorf("upcoming is soonest first: %+v", d.upcoming)
	}
	if time.Since(d.lastPosted) < 2*time.Hour || time.Since(d.lastPosted) > 4*time.Hour {
		t.Errorf("lastPosted = %v", d.lastPosted)
	}
	if after := sha256.Sum256(mustRead(t, db)); after != before {
		t.Error("the dashboard must open postctl's database read-only")
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPostctlStatusBranches(t *testing.T) {
	fakeSuite(t, map[string][]fakeRule{})
	home, _ := os.UserHomeDir()
	if got := postctlStatus(time.Now()); got.text != "–  not configured" {
		t.Errorf("no database: %q", got.text)
	}
	db := filepath.Join(home, ".local", "share", "postctl", "postctl.db")

	os.Remove(db)
	mkPostctlDB(t, db)
	got := postctlStatus(time.Now())
	if got.text != "0 scheduled" || got.left[0] != "nothing scheduled" || got.right[1] != "✓ all clear" || got.right[2] != "never posted" || got.urgency != urgencyNormal {
		t.Errorf("empty queue: %+v", got)
	}

	os.Remove(db)
	mkPostctlDB(t, db, fmt.Sprintf(`('1','x','Late','scheduled','%s','','')`, rfc(-time.Hour)))
	if got := postctlStatus(time.Now()); got.urgency != urgencyWarn || !strings.Contains(got.right[1], "1 overdue") {
		t.Errorf("overdue: %+v", got)
	}

	os.Remove(db)
	mkPostctlDB(t, db,
		fmt.Sprintf(`('1','x','Late','scheduled','%s','','')`, rfc(-time.Hour)),
		fmt.Sprintf(`('2','x','Bad','failed','','','%s')`, time.Now().UTC().Format("2006-01-02 15:04:05")),
	)
	if got := postctlStatus(time.Now()); got.urgency != urgencyCritical || !strings.Contains(got.right[1], "1 failed") {
		t.Errorf("a failure outranks an overdue post: %+v", got)
	}
}

func TestPostctlWhenAndLine(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	at := func(d, h int) time.Time { return time.Date(2026, 10, 19+d, h, 30, 0, 0, time.Local) }
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{at(0, 18), "today 18:30"},
		{at(1, 9), "tomorrow 09:30"},
		{at(3, 9), "Thu 22.10. 09:30"},
	} {
		if got := postctlWhen(tc.t, now); got != tc.want {
			t.Errorf("%v → %q, want %q", tc.t, got, tc.want)
		}
	}
	if got := postctlLine(postctlPost{profile: "bruecke", platform: "instagram", at: at(0, 18)}, now); got != "today 18:30 · IG (bruecke)" {
		t.Errorf("line = %q", got)
	}
	if got := postctlLine(postctlPost{platform: "tiktok", at: at(1, 9)}, now); got != "tomorrow 09:30 · tiktok" {
		t.Errorf("an unknown platform keeps its name: %q", got)
	}
}
