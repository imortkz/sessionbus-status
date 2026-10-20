# bus-status

An interactive terminal tree of Sessionbus peers and their lanes. One tab shows each session's harness, self-reported model, current activity and age, with status history or a live native log below the tree. Questions waiting for the owner appear first with a red `⚠`, an audible macOS sound and a system notification.

Build using cached dependencies:

```sh
GOCACHE=$PWD/.gocache GOPROXY=off GOFLAGS=-mod=mod go build -o bin/bus-status .
```

Start the TUI and collector together:

```sh
bin/bus-status
# Include another shared group if needed:
bin/bus-status -g bus-status,openclaw-peer
```

On the first successful roster refresh, a connected local `bus-status` collector makes the TUI display-only (`collector: external`). Otherwise the TUI starts its own collector (`collector: own`) and stops it on exit. An external collector disappearing changes the header to `collector: not found`; restart the TUI to make a new collector decision.

In TUI mode, collector diagnostics append to `collector.log` beside the state file. The header shows `collector: own (connected)` after connection, or the latest collector error. Headless `serve` keeps writing diagnostics to stderr.

Senders must share a group with the collector. Launch an interactive master with `-g bus-status` (for example, `claude-peer -g openclaw-peer -g bus-status`); lanes join through `extra_groups`. Messages go directly to `bus-status@<host>`, so group membership does not give other peers extra turns. Both the TUI and headless collector accept comma-separated groups through `-g` (default `bus-status`).

For a single text frame or a headless collector suitable for a separate terminal or launchd:

```sh
bin/bus-status -once
bin/bus-status serve -g bus-status
```

`-once` prints the same ordered tree without starting a collector or playing sounds. `watch` is an alias for the TUI. The `serve` command remains available; launchd setup is left to the owner.

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

The tree uses a shared table in the TUI and `-once`: session, status, age, shortened model and harness. Child session names omit the parent prefix.

The bottom pane shows the last 20 status, ask and model updates, persisted across restarts. Rows keep their normal color regardless of age. Non-running lanes show `idle` in the AGE cell (for example, `idle 59m`); their status stays unchanged. Idle peers with no lanes are hidden when a session on the same host has the same name and a newer status; the header shows `hidden N`, and `f` reveals them. Active or waiting peers stay visible. Names duplicated among visible sessions get a short ID suffix; unnamed sessions show their harness and short ID. The help stays visible at the bottom, including at 60×20.

The log pane follows the last 200 parsed lines for local Claude, Codex and DSH sessions. It reads native files under `~/.claude/projects`, `~/.codex/sessions` or `~/.codex/archived_sessions`, and `~/.dsh/sessions` (zstd), respectively. Records use time, kind and text columns. `TEXT` is green, `TOOL` cyan with a highlighted tool name, `RESULT` dim, `THINK` yellow and `SYS` magenta when color is enabled. Long text wraps under the text column, including CJK and emoji. The parser retains up to 2000 runes per record (400 for tool arguments). The pane follows the tail while at the bottom; scrolling up holds the view and shows `▼ N` new records at the bottom-right. `G` returns to the tail. The help changes with focus. Remote-host logs, missing or unreadable files, and unknown products show an explanation in the pane.

On startup, existing asks are remembered silently; their next reminder is due after one minute. A new pending ask plays `/System/Library/Sounds/Glass.aiff` and posts a `bus-status` notification with `<name> asks: <text>`. While asks remain, a reminder repeats every minute for the oldest pending ask. `-no-sound` starts with both sound and notifications muted; `-sound PATH` selects a different audio file. Unmuting does not replay every old ask. Notifications require macOS to allow them for the invoking application.

Other TUI and `-once` options: `-interval 2s` (roster polling), `-roster-timeout 10s` (default: the greater of the interval and 10s), `-state PATH`, and `-roster "sessionbus roster --json"`. State and the selected live log refresh once per second (DSH logs at most once every three seconds). The default state file is `$XDG_STATE_HOME/bus-status/state.json`, or `~/.local/state/bus-status/state.json`; an external collector and the TUI must use the same path. All generated UI text is English; status, ask and log text from agents is preserved as supplied. Color requires a terminal and an unset `NO_COLOR`.

Message protocol: send one or more lines with the Sessionbus tool:

- `status: <text>` — current activity; also clears the sender's ask.
- `ask: <text>` — the question awaiting the owner.
- `model: <id>` — self-reported model; send with the first status.

Prefixes are case-insensitive after trimming leading spaces. Unknown lines and empty values are ignored. Updates in a body apply in order; protocol text is cleaned and capped at 120 runes.

```json
{"action":"send","arguments":{"target":"bus-status@macbook","message":"model: gpt-6.1-sol\nstatus: implementing the collector"}}
```

Only master sends `ask:` for owner questions. Lanes waiting for master send `status:` instead. Status and model are self-reported; there is no automatic activity detection. Remote roster hosts appear in the tree, but their statuses require a collector reachable by those senders.

State v2 keeps history and loads older v1 files. State is synced and atomically written with mode 0600 in a directory created with mode 0700. If a collector cannot load its state file, it warns, preserves the file as `state.json.corrupt-<unix>` when possible, and starts empty.

Offline checks:

```sh
GOCACHE=$PWD/.gocache GOPROXY=off GOFLAGS=-mod=mod go test ./...
GOCACHE=$PWD/.gocache GOPROXY=off GOFLAGS=-mod=mod go vet ./...
```
