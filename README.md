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

On the first successful roster refresh, a connected local `bus-status` collector makes the TUI display-only (`сборщик: внешний`). Otherwise the TUI starts its own collector (`сборщик: свой`) and stops it on exit. An external collector disappearing changes the header to `сборщик: не найден`; restart the TUI to make a new collector decision.

Senders must share a group with the collector. Launch an interactive master with `-g bus-status` (for example, `claude-peer -g openclaw-peer -g bus-status`); lanes join through `extra_groups`. Messages go directly to `bus-status@<host>`, so group membership does not give other peers extra turns. Both the TUI and headless collector accept comma-separated groups through `-g` (default `bus-status`).

For a single text frame or a headless collector suitable for a separate terminal or launchd:

```sh
bin/bus-status -once
bin/bus-status serve -g bus-status
```

`-once` prints the same ordered tree without starting a collector or playing sounds. `watch` is an alias for the TUI. The `serve` command remains available; launchd setup is left to the owner.

| Key | Action |
|---|---|
| ↑ / ↓ | Select a session; the tree follows the selection |
| ← / → | Collapse / expand its children |
| l | Toggle status history / live log |
| L | Give the live log the full body |
| Esc | Return to history |
| f | Hide peers with no status and no lanes |
| s | Mute / unmute sound and notifications |
| q / Ctrl+C | Quit |

The bottom pane shows the last 20 status, ask and model updates, persisted across restarts. Idle sessions whose last update is over ten minutes old are dimmed. Duplicate names get a short ID suffix; unnamed sessions show their harness and short ID. The help stays visible at the bottom, including at 60×20.

The log pane follows the last 200 parsed lines for local Claude, Codex and DSH sessions. It reads native files under `~/.claude/projects`, `~/.codex/sessions` or `~/.codex/archived_sessions`, and `~/.dsh/sessions` (zstd), respectively. Records are labeled `TEXT`, `THINK`, `TOOL`, `RESULT` or `SYS`. Remote-host logs, missing or unreadable files, and unknown products show an explanation in the pane.

A new pending ask plays `/System/Library/Sounds/Glass.aiff` and posts a `bus-status` notification containing the question. While asks remain, a reminder repeats every minute for the oldest pending ask. `-no-sound` starts with both sound and notifications muted; `-sound PATH` selects a different audio file. Unmuting does not replay every old ask. Notifications require macOS to allow them for the invoking application.

Other TUI and `-once` options: `-interval 2s` (roster polling), `-roster-timeout 10s` (default: the greater of the interval and 10s), `-state PATH`, and `-roster "sessionbus roster --json"`. State and the selected live log refresh once per second. The default state file is `$XDG_STATE_HOME/bus-status/state.json`, or `~/.local/state/bus-status/state.json`; an external collector and the TUI must use the same path. Text-frame color requires a terminal and an unset `NO_COLOR`.

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
