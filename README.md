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

# Dashboard — briefing across all tools, jump into any of them with 1-9
missionctl

# Check the suite's health
missionctl doctor

# Daily briefing across all tools (plain text, for scripts/piping)
missionctl status

# Interactive setup wizard
missionctl init

# Search tasks, events, notes, mail, budget, diary, time and habits at once
missionctl search steuer

# AI day plan / weekly review (prints only, changes nothing)
missionctl plan
missionctl review

# macOS banners: event in 10 min, streak at risk, tasks due
missionctl notify --dry-run
```

---

## Cheatsheet

| Command             | What it does                                                        |
|----------------------|----------------------------------------------------------------------|
| `missionctl`         | Dashboard TUI — same briefing as `status`, press 1-9/enter to jump into a tool, `/` to search |
| `missionctl doctor`  | Installed tools, env vars, MCP registration, DB freshness, daemons  |
| `missionctl status`  | Daily briefing: tasks, calendar, timer, diary, budget, habits, notes, mail |
| `missionctl log`     | Activity log: what you did across the suite (`--since`, `--tool`, `--json`) |
| `missionctl search`  | Search all tools at once (`--per-tool N`, `--json`)                 |
| `missionctl plan`    | AI day plan from calendar, open tasks, habits (`--lang`, `--show-prompt`) |
| `missionctl review`  | AI weekly review across time, tasks, habits, budget, diary          |
| `missionctl notify`  | macOS notifications, once each (`--dry-run`, `--install`, `--uninstall`) |
| `missionctl init`    | Interactive wizard: API key, Obsidian vault path, install missing tools |
| `missionctl install` | Build + install every tool via its `setup.sh` (`--all` to reinstall) |
| `missionctl update`  | `git pull` + rebuild every tool from its local checkout             |

---

## CLI Reference

### `missionctl` (no arguments)

Opens a Bubble Tea dashboard: a grid of cards (two per row), one per tool,
using the exact same DB-reading logic as `status`. The suite's tools are
launched from here — the dashboard suspends itself (via `tea.ExecProcess`)
while a tool runs and refreshes when it exits.

**Keys**

| Key | Action |
|-----|--------|
| `↑` `↓` `←` `→` / `h` `j` `k` `l` | Move between cards |
| `1`-`9` | Jump into that tool (numbers follow the cards on screen) |
| `enter` | Open the selected card's tool |
| `x` | Quick action on the selected card: complete the first task (Tasks), stop the running timer (Timer), check the first open habit (Habits) |
| `s` | Sync all tools one after another |
| `r` | Reload now |
| `a` | Today's agenda (calendar + due tasks + timer sessions in one timeline); `a` again goes back |
| `d` / `space` | Peek: read-only list of the selected card's items (open tasks, today's events, timer sessions, habits, recent notes) without leaving the dashboard — `enter` opens the tool, `esc` closes |
| `/` | Search across all tools (see below) |
| `?` | Key reference popup (any key closes it) |
| `L` | License settings (activate a Bundle key for every tool at once) |
| `q` / `esc` | Quit |

**Mouse**: click selects a card, a quick second click (double-click) opens its
tool, hovering draws a double border around the card under the pointer.

**Refreshing**: all cards load in parallel in the background, so the dashboard
never freezes — a card without data yet says `loading…`, and the footer shows
`↻ refreshing…` while a refresh runs. Data reloads every 30 seconds, after a
launched tool exits, after a quick action or sync, and when the terminal window
regains focus (at most every 5 seconds). A card turns amber/red when something
needs attention (overdue tasks, a blown budget goal).

**Sparklines**: the Tasks card shows a 7-bar sparkline of tasks completed per day
and the Timer card the minutes tracked per day over the last 7 days (`▁▂▃▄▅▆▇█`,
right-aligned on the detail line). The Habits card has none: `habctl` only
reports today's status, not a per-day history.

**Choosing cards**: create `dashboard.yaml` in missionctl's config directory
(`~/.config/missionctl/`) to show only some cards,
in your order:

```yaml
cards: [tasks, calendar, habits, notes]
```

Valid ids are the lower-case card names: `tasks`, `calendar`, `timer`, `diary`,
`budget`, `habits`, `notes`, `mail`. Unknown ids are ignored; a missing file or a
list with no valid id shows all eight cards. The number keys always match the
cards that are visible (the first nine get `1`-`9`).

### `missionctl doctor`

Reports, for every tool in the suite:
- Whether the binary is on `PATH`, and the install command if not
- Required/optional environment variables (`ANTHROPIC_API_KEY`, `TIMECTL_GOAL_HOURS`, `TIMECTL_HOURLY_RATE`)
- Whether it's registered as an MCP server in `~/.claude.json`
- Its SQLite database's last-modified time (i.e. last sync), plus a `PRAGMA quick_check` integrity result (database opened read-only)
- The tool's own `<tool> doctor` self-check (5 s timeout): ✓/✗ with the first failing line; a ✗ line counts as failed even when the exit code is 0
- Whether its launchd daemon (diaryctl, taskctl) is installed and loaded

Exits non-zero if any tool is missing, so it can be used in scripts.

### `missionctl status`

Prints a one-line-per-tool briefing by reading each tool's local SQLite
database directly (read-only, no network): open/due tasks, today's calendar
events, running timer, diary streak, this month's spending, habit
check-ins, note count, and unread mail. A tool that isn't installed or
hasn't synced yet just shows as "not configured" — nothing errors.

### `missionctl search QUERY [--per-tool N] [--json]`

Searches every installed tool at once and prints the hits grouped by tool
(default 5 per tool; `--json` prints a flat array of
`{tool, icon, title, snippet, when}`). The same search is available inside the
dashboard with `/`: type a query and press `enter`; once there are hits the focus
moves into the results (`↑` `↓` / `j` `k` to move, `enter` opens that hit's tool —
it jumps into the tool, not to the item), `/` or `tab` (or `↑` on the first hit)
goes back to the query, `esc` closes the search.

It asks each tool through its own `--json` output, so every tool's data directory
and profile settings are respected and no database is read directly:

| Tool | How it is searched |
|------|--------------------|
| notectl, mailctl, budgetctl | the tool's own search (`notectl search`, `mailctl search`, `budgetctl list -q`) — mailctl searches the local cache, not the mail client |
| taskctl | all tasks including completed ones, filtered here |
| calctl | events from 30 days back to 90 days ahead, filtered here |
| diaryctl | the last 200 entries, filtered here |
| timectl | the last 90 days of the time log, filtered here |
| habctl | today's habit list, filtered here |

For the filtered tools the query is a case-insensitive substring match over every
text field. A tool that isn't installed, errors out or takes longer than 8
seconds is skipped.

### `missionctl plan [--lang LANG] [--show-prompt]`

An AI plan for the rest of today, built from your calendar and due tasks, open
tasks (first 15) and the habits you haven't checked in yet. It **only prints** —
nothing is written to any tool. `--show-prompt` prints exactly what would be sent
instead of calling the AI. `--lang German` (or any language) fixes the answer
language; the default `auto` answers in the language your items are written in.

### `missionctl review [--lang LANG] [--show-prompt]`

A weekly review from compact snapshots of timectl (last 7 days), taskctl (this
week), habctl, budgetctl (summary) and diaryctl (last 7 entries). Each snapshot is
cut at 3,500 characters and tools that aren't installed are left out. Same flags
and print-only behaviour as `plan`.

**AI provider** (both commands): the first key found wins — `ANTHROPIC_API_KEY`,
`OPENAI_API_KEY`, `GEMINI_API_KEY` — else a local Ollama. Set
`MISSIONCTL_PROVIDER=anthropic|openai|gemini|ollama` to force one
(`OLLAMA_MODEL` / `MISSIONCTL_OLLAMA_MODEL` pick the local model).

### `missionctl notify [--dry-run] [--install] [--uninstall]`

Checks calendar, habits and tasks once and posts a macOS banner for what needs
attention:

| Banner | When |
|--------|------|
| **Upcoming** | a timed event starts within the next 10 minutes (also "now") |
| **Streak at risk** | after 18:00, a habit with a streak that isn't checked in today |
| **Tasks** | from 09:00, a digest of tasks due today or overdue |

Each banner fires only once (event: per event, habit: per day, digest: per day);
what was announced is remembered for 48 hours in `notified.json` in
`~/.config/missionctl/`. `--dry-run` prints what would be
announced and neither posts nor remembers anything.

Run it automatically every 5 minutes:

```bash
missionctl notify --install      # writes ~/Library/LaunchAgents/sh.missionctl.notify.plist
launchctl load ~/Library/LaunchAgents/sh.missionctl.notify.plist
missionctl notify --uninstall    # unloads and removes it again
```

`--install` only writes the file and prints the `launchctl load` line; it does
not start anything. Output goes to `~/Library/Logs/missionctl/notify.log`.

### `missionctl log [--since WINDOW] [--tool TOOL] [--json]`

The suite's **activity log**: every tool writes one line when you add, complete,
delete, check, start, stop, write, send or publish something — `missionctl log`
shows it, grouped by day (newest day first).

```sh
missionctl log                        # today
missionctl log --since yesterday
missionctl log --since 7d --tool taskctl
missionctl log --since 2026-10-01 --json
```

`--since` takes `today`, `yesterday`, `Nd`, `Nw` or `YYYY-MM-DD`. The log lives in
`~/.local/share/missionctl/activity.jsonl` (override with `MISSIONCTL_DATA_DIR`).
**Only titles are logged** — never note bodies, mail text, recipients or amounts
(budgetctl logs just "a transaction" / "N transactions").

Settings (`~/.config/missionctl/activity.yaml`):

| Command | Effect |
|---|---|
| `missionctl log --disable` / `--enable` | Stop / resume logging for every tool (`MISSIONCTL_ACTIVITY=off` disables it too) |
| `missionctl log --diary ask` | diaryctl offers to add the day's activity to your diary after 18:00 (default) |
| `missionctl log --diary auto` | diaryctl adds it automatically when the day-end entry is generated |
| `missionctl log --diary off` | never |

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

## Trying changes safely

Don't want `setup.sh` to touch `~/.local/bin` and `~/.claude.json` while you test?
See [`TESTING.md`](../TESTING.md) in the umbrella repo: `scripts/dev-build.sh`
builds every tool into `.dev/bin`, `source scripts/dev-env.sh` switches your
shell to a throw-away `HOME`, `scripts/test-all.sh` runs the whole suite
isolated from your real data, and `scripts/first-run.sh` checks that every tool
works on a machine with no data directory yet.

---

## Requirements

- Go 1.22+
- macOS (the suite's tools use AppleScript/EventKit integrations)
- The tools you want `doctor`/`status`/`install`/`update` to manage, cloned
  as submodules under this repo (see `git-deploy.md` in the root `missionctl` repo)

---

## License

MIT
