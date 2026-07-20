# missionctl

Umbrella CLI for the missionctl suite — the control plane for mailctl, calctl,
taskctl, notectl, budgetctl, habctl, timectl, diaryctl and postctl. Checks
what's installed, gives a daily briefing across every tool's database, and
installs/updates the whole suite in one command.

---

## Quick Start

```bash
# Build & install
cd missionctl
chmod +x setup.sh
./setup.sh

# Check the suite's health
missionctl doctor

# Daily briefing across all tools
missionctl status

# Interactive setup wizard
missionctl init
```

---

## Cheatsheet

| Command             | What it does                                                        |
|----------------------|----------------------------------------------------------------------|
| `missionctl doctor`  | Installed tools, env vars, MCP registration, DB freshness, daemons  |
| `missionctl status`  | Daily briefing: tasks, calendar, timer, diary, budget, habits, notes, mail |
| `missionctl init`    | Interactive wizard: API key, Obsidian vault path, install missing tools |
| `missionctl install` | Build + install every tool via its `setup.sh` (`--all` to reinstall) |
| `missionctl update`  | `git pull` + rebuild every tool from its local checkout             |

---

## CLI Reference

### `missionctl doctor`

Reports, for every tool in the suite:
- Whether the binary is on `PATH`, and the install command if not
- Required/optional environment variables (`ANTHROPIC_API_KEY`, `TIMECTL_GOAL_HOURS`, `TIMECTL_HOURLY_RATE`)
- Whether it's registered as an MCP server in `~/.claude.json`
- Its SQLite database's last-modified time (i.e. last sync)
- Whether its launchd daemon (diaryctl, taskctl) is installed and loaded

Exits non-zero if any tool is missing, so it can be used in scripts.

### `missionctl status`

Prints a one-line-per-tool briefing by reading each tool's local SQLite
database directly (read-only, no network): open/due tasks, today's calendar
events, running timer, diary streak, this month's spending, habit
check-ins, note count, and unread mail. A tool that isn't installed or
hasn't synced yet just shows as "not configured" — nothing errors.

### `missionctl init`

Interactive wizard for a fresh machine: prompts for `ANTHROPIC_API_KEY` and
an Obsidian vault path (for notectl), then offers to install any missing
tools via their `setup.sh`.

### `missionctl install [--all]`

Runs `setup.sh` for every tool not currently on `PATH`. Pass `--all` to
rebuild and reinstall every tool regardless of whether it's already
installed.

### `missionctl update`

For each tool with a local checkout under
`~/Developing/Projects/missionctl/<tool>`: `git pull --ff-only`, then
re-runs `setup.sh` to rebuild and reinstall. This targets the
source-checkout distribution model; a Homebrew-tap-based update path can be
layered in once the tap is public (see `ROADMAP.md`).

---

## Requirements

- Go 1.22+
- macOS (the suite's tools use AppleScript/EventKit integrations)
- The tools you want `doctor`/`status`/`install`/`update` to manage, cloned
  as submodules under this repo (see `git-deploy.md` in the root `missionctl` repo)

---

## License

MIT
