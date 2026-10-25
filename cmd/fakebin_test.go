package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRule makes a stub tool answer one exact argument list ("today --json").
type fakeRule struct {
	args string
	out  string
	code int
}

// fakeSuite installs executable stubs for the given tools and restricts PATH to them plus
// /usr/bin:/bin (for sh and cat), so a test can never reach a real tool or real data.
// Every call is logged as `name <arg1> <arg2> ...`; calls() returns the log so far.
// An argument list without a rule exits 0 silently.
func fakeSuite(t *testing.T, tools map[string][]fakeRule) (calls func() []string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	for name, rules := range tools {
		var b strings.Builder
		b.WriteString("#!/bin/sh\n")
		b.WriteString("printf '%s' '" + name + "' >> '" + logPath + "'\n")
		b.WriteString("for a in \"$@\"; do printf ' <%s>' \"$a\" >> '" + logPath + "'; done\n")
		b.WriteString("if [ ! -t 0 ]; then IFS= read -r line; [ -n \"$line\" ] && printf ' stdin=<%s>' \"$line\" >> '" + logPath + "'; fi\n")
		b.WriteString("printf '\\n' >> '" + logPath + "'\n")
		b.WriteString("case \"$*\" in\n")
		for _, r := range rules {
			b.WriteString("  '" + strings.ReplaceAll(r.args, "'", `'\''`) + "')\n")
			if r.out != "" {
				b.WriteString("cat <<'__EOF__'\n" + r.out + "\n__EOF__\n")
			}
			b.WriteString("    exit " + string(rune('0'+r.code)) + " ;;\n")
		}
		b.WriteString("esac\nexit 0\n")
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	return func() []string {
		raw, _ := os.ReadFile(logPath)
		var out []string
		for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if l != "" {
				out = append(out, l)
			}
		}
		return out
	}
}
