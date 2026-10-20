# sessionbus-status

[![ci](https://github.com/imortkz/sessionbus-status/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/imortkz/sessionbus-status/actions/workflows/ci.yml)
[![security](https://github.com/imortkz/sessionbus-status/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/imortkz/sessionbus-status/actions/workflows/security.yml)
[![release](https://github.com/imortkz/sessionbus-status/actions/workflows/release.yml/badge.svg)](https://github.com/imortkz/sessionbus-status/actions/workflows/release.yml)
[![latest release](https://img.shields.io/github/v/release/imortkz/sessionbus-status?include_prereleases&sort=semver)](https://github.com/imortkz/sessionbus-status/releases)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An interactive terminal tree of [Sessionbus](https://github.com/antst/sessionbus) peers and their lanes. One tab shows each session's harness, self-reported model, current activity and age, with status history or a live native log below the tree. Questions waiting for the owner appear first with a red `⚠`, an audible macOS sound and a system notification.

Status: alpha (`v0.1.0-alpha.1`). Interfaces may change.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/imortkz/sessionbus-status/main/scripts/install.sh | sh
```

The installer downloads the newest release (prereleases included) for your platform (Linux or macOS, amd64 or arm64), verifies it against `SHA256SUMS`, and installs `~/.local/bin/sessionbus-status`. Pin a version with `SESSIONBUS_STATUS_VERSION`:

```sh
curl -fsSL https://raw.githubusercontent.com/imortkz/sessionbus-status/main/scripts/install.sh | SESSIONBUS_STATUS_VERSION=v0.1.0-alpha.1 sh
sessionbus-status --version
```

## Requirements

- Sessionbus 0.5.12 or newer with `sessionbus roster --json`.
- Sound and notifications work on macOS only; this is a stated limitation of the repository. On Linux the TUI runs without them.
- Optional, macOS: `brew install terminal-notifier`, then allow notifications for terminal-notifier in System Settings → Notifications. Clicking a notification opens the iTerm2 tab of the session that asked, if that session sent `tab:` (local sessions only); otherwise it brings the terminal running the TUI to the front. On the first such click macOS asks to let terminal-notifier control iTerm2 (System Settings → Privacy & Security → Automation → terminal-notifier → iTerm2). Without terminal-notifier (or without that permission) the built-in osascript notification is used, and clicking it opens Script Editor.

## Quick start

Start the TUI and collector together:

```sh
sessionbus-status
# Include another shared group if needed:
sessionbus-status -g bus-status,demo-group
```

On the first successful roster refresh, a connected local `bus-status` collector makes the TUI display-only (`collector: external`). Otherwise the TUI starts its own collector (`collector: own`) and stops it on exit. An external collector disappearing changes the header to `collector: not found`; restart the TUI to make a new collector decision.

In TUI mode, cleaned collector diagnostics append to `collector.log` beside the state file. At 1 MiB it rotates to `collector.log.1`, retaining one previous generation. The header keeps a short collector state (`own`, `external`, `checking…`, `not found` or `error`) so the sound icon and waiting count remain visible. Full collector errors appear in the body. Headless `serve` keeps writing diagnostics to stderr.

The collector keeps its Sessionbus identity `bus-status` (address `bus-status@<host>`, default group `bus-status`); only the binary and the TUI are named `sessionbus-status`. Senders must share a group with the collector. Launch an interactive master with `-g bus-status` (for example, `claude-peer -g bus-status`); lanes join through `extra_groups`. Messages go directly to `bus-status@<host>`, so group membership does not give other peers extra turns. Both the TUI and headless collector accept comma-separated groups through `-g` (default `bus-status`).

For a single text frame or a headless collector suitable for a separate terminal or launchd:

```sh
sessionbus-status -once
sessionbus-status serve -g bus-status
```

`-once` prints the same ordered tree without starting a collector or playing sounds. `watch` is an alias for the TUI. The `serve` command remains available; launchd setup is left to the owner. `sessionbus-status --version` (or `version`) prints the release and revision.

## Keys and behaviour

| Key | Action |
|---|---|
| ↑ / ↓ | Select a session in tree focus; scroll one line in log focus |
| ← / → | Collapse / expand its children |
| l | Toggle status history / live log |
| Tab | Switch focus between tree and log |
| L | Give the live log the full body and focus it |
| PgUp / PgDn | Scroll a page in log focus |
| g / G | Go to the top / resume following the tail in log focus |
| Esc | Return to tree focus and the split view |
| f | Show all sessions / hide stale duplicate peers |
| s | Mute / unmute sound and notifications |
| q / Ctrl+C | Quit |

The tree uses a shared table in the TUI and `-once`: session, status, age, shortened model and harness. Session names omit a trailing `@host` matching their row’s host because rows are grouped by host; other `@` characters are preserved. Child session names also omit the parent prefix.

The bottom pane shows the last 20 status, ask and model updates, persisted across restarts. Rows keep their normal color regardless of age. Non-running lanes show `idle` in the AGE cell (for example, `idle 59m`); their status stays unchanged. Idle peers with no lanes are hidden when a session on the same host has the same name and appeared later in this TUI (or, failing that, has a newer status), or when they are disconnected and a connected session has the same name; the header shows `hidden N`, and `f` reveals them. Active or waiting peers stay visible. Names duplicated among visible sessions get a short ID suffix; unnamed sessions show their harness and short ID. The help stays visible at the bottom, including at 60×20.

The log pane follows the last 200 parsed lines for local Claude, Codex and DSH sessions. It reads native files under `~/.claude/projects`, `~/.codex/sessions` or `~/.codex/archived_sessions`, and `~/.dsh/sessions` (zstd), respectively. Records use time, kind and text columns. `TEXT` is green, `TOOL` cyan with a highlighted tool name, `RESULT` dim, `THINK` yellow and `SYS` magenta when color is enabled. Long text wraps under the text column, including CJK and emoji. The parser retains up to 2000 runes per record (400 for tool arguments). The pane follows the tail while at the bottom; scrolling up holds the view and shows `▼ N` new records at the bottom-right. `G` returns to the tail. The help changes with focus. Remote-host logs, missing or unreadable files, and unknown products show an explanation in the pane.

On startup, existing asks are remembered silently; their next reminder is due after one minute. A new pending ask plays `/System/Library/Sounds/Glass.aiff` and posts a `sessionbus-status` notification with `<name> asks: <text>`. The name matches the SESSION column, with ` (on <host>)` added for remote sessions. Simultaneous asks from different sessions produce separate notifications; a reminder replaces that session's earlier one. While asks remain, a reminder repeats every minute for the oldest pending ask. `-no-sound` starts with both sound and notifications muted; `-sound PATH` selects a different audio file. Unmuting does not replay every old ask. With `terminal-notifier` installed, notifications go through it; allow notifications for terminal-notifier in System Settings → Notifications, otherwise the built-in osascript notification is used as a fallback.

Other TUI and `-once` options: `-interval 2s` (roster polling), `-roster-timeout 10s` (default: the greater of the interval and 10s), `-state PATH`, and `-roster "sessionbus roster --json"`. State and the selected live log refresh once per second (DSH logs at most once every three seconds). The default state file is `$XDG_STATE_HOME/sessionbus-status/state.json`, or `~/.local/state/sessionbus-status/state.json`; an external collector and the TUI must use the same path. On first start, if that file does not exist and the previous `bus-status/state.json` does, it is copied once (the old file stays; an explicit `-state` is never migrated). A `bus-status serve` still running from an older build keeps writing the old path: restart it with the new binary so it shares the new one. All generated UI text is English; status, ask and log text from agents is preserved as supplied. Color requires a terminal and an unset `NO_COLOR`.

Message protocol: send one or more lines with the Sessionbus tool:

- `status: <text>` — current activity; also clears the sender's ask.
- `ask: <text>` — the question awaiting the owner.
- `model: <id>` — self-reported model; send with the first status.
- `tab: <ITERM_SESSION_ID>` — the sender's iTerm2 session; send with the first status. Clicking that sender's ask notification selects this tab.
- `name: <text>` — display name for a session the bus knows only by id; a bus name always wins.

For `tab:` send `$ITERM_SESSION_ID`, whole or just the GUID after the colon; an invalid value is ignored. `tab:` and `name:` are not shown in the status history.

Prefixes are case-insensitive after trimming leading spaces. Unknown lines and empty values are ignored. Updates in a body apply in order; protocol text is cleaned and capped at 120 runes.

```json
{"action":"send","arguments":{"target":"bus-status@laptop","message":"model: gpt-6.1-sol\nstatus: implementing the collector"}}
```

Only master sends `ask:` for owner questions. Lanes waiting for master send `status:` instead. Status and model are self-reported; there is no automatic activity detection. Remote roster hosts appear in the tree, but their statuses require a collector reachable by those senders.

State v2 keeps history and loads older v1 files. State is synced and atomically written with mode 0600 in a directory created with mode 0700. If a collector cannot load its state file, it warns, preserves the file as `state.json.corrupt-<unix>` when possible, and starts empty.

## Build from source

```sh
go build -o bin/sessionbus-status .
```

With a pre-populated module cache and no network:

```sh
GOCACHE=$PWD/.gocache GOPROXY=off GOFLAGS=-mod=mod go build -o bin/sessionbus-status .
```

Run the checks:

```sh
go test ./...
go vet ./...
```

## License

MIT; see `LICENSE`.

