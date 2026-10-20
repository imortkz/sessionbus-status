# bus-status

A terminal tree of Sessionbus peers and their lanes, showing each session's harness, self-reported model, current activity and age. Questions waiting for a human appear first with a red `⚠`; a new question rings the terminal bell once.

Build with the cached dependencies (all Go commands can run offline with `GOCACHE=$PWD/.gocache GOPROXY=off`):

```sh
GOCACHE=$PWD/.gocache GOPROXY=off go build -o bin/bus-status .
```

Run the collector in one terminal, including your project groups:

```sh
bin/bus-status serve -g bus-status,openclaw-peer
```

Run the viewer in another:

```sh
bin/bus-status
# Or print a single frame:
bin/bus-status -once
```

`watch` is the default command. Options: `-interval 2s`, `-roster-timeout 10s` (default: the greater of the interval and 10s), `-once`, `-state PATH`, `-roster "sessionbus roster --json"`. Both processes accept `-state PATH`; their default is `$XDG_STATE_HOME/bus-status/state.json`, or `~/.local/state/bus-status/state.json`. Color requires a terminal and an unset `NO_COLOR`.

Message protocol (what agents send with Sessionbus `send` to target `bus-status@<host>`), one or more lines:
- `status: <text>` — what I am doing now; also clears my ask.
- `ask: <text>` — I am waiting for a human; text = the question in a few words.
- `model: <id>` — my model (e.g. `gpt-6.1-sol`); send with the first status.
Prefix match is case-insensitive after trimming leading spaces; lines without a known prefix are ignored; empty text after
the prefix is ignored. When one body contains several lines, they apply in order.

For example, call the Sessionbus tool:

```json
{"action":"send","arguments":{"target":"bus-status@macbook","message":"model: gpt-6.1-sol\nstatus: implementing the collector"}}
```

Only master sends `ask:` for questions to the owner. Lanes waiting for master send `status:` instead.

Sessionbus visibility requires a shared group. The collector's `-g` list must include a group shared with every sender: lanes get `bus-status` through `extra_groups`, while interactive peers share their project group (for example `openclaw-peer`).

Limits: status and model are self-reported only; there is no automatic detection. State is a macbook-local file, synced to disk and written atomically with mode 0600 in a directory created with mode 0700. Remote roster hosts appear in the tree, but their statuses require a collector reachable by those senders. The viewer reads the roster and state; keep the collector running to receive updates.

If the collector cannot load its state file, it logs a warning, moves the file to `state.json.corrupt-<unix>` when possible, and starts empty.
