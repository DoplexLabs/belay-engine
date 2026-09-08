# Belay Local V0 Launch Requirements

Status: implementation contract for the individual-developer launch slice.

## Product promise

Belay Local gives one developer a private, offline view of activity across the
AI agent harnesses present on their machine. It reconstructs supported history,
continues collecting supported live activity after explicit hook installation,
and exposes the same read-only evidence through a local browser and MCP.

Belay Local has no account, hosted dependency, product telemetry, model
inference, write-capable MCP tool, or Teams requirement.

## Launch scope

### Required

1. Resolve and verify a pinned, unmodified Numbat executable.
2. Inventory every harness Numbat reports on the machine.
3. Scan the launch-validated Codex and Claude Code artifact stores and import
   Numbat `--emit all` NDJSON through the existing strict adapter.
4. Preserve historical/live deduplication and deterministic timeline ordering.
5. Serve a loopback-only Local API and embedded browser timeline.
6. Serve a read-only stdio MCP adapter over the Local read model.
7. Offer explicit monitor-only live-hook install, status, and uninstall.
8. Keep Local usable with the network disabled.
9. Provide an installable macOS development package and reproducible build
   instructions.
10. Publish exact coverage and known limitations.

### Launch-validated harnesses

- Codex
- Claude Code

Numbat may discover and parse additional supported harnesses. They are reported
as upstream-supported until Belay adds a sanitized end-to-end launch fixture.

### Deferred

- Belay Teams enrollment, upload, API, UI, billing, or deployment
- Write-capable MCP, remediation, fix recording, or recurrence registration
- Belay-hosted model inference
- Enforcement or blocking mode
- Windows packaging
- Linux packaging beyond reproducible source builds

## User commands

The launch CLI must converge on these stable workflows:

```text
belay local
belay local --install-hooks
belay scan
belay agents
belay hooks status|install|uninstall
belay mcp
belay doctor
```

`belay local` initializes Local state if necessary, performs a historical scan,
imports any new live records, starts the loopback API, and prints the local URL.
It never edits an agent configuration unless `--install-hooks` was explicitly
provided.

## Local filesystem contract

Default root: `${BELAY_HOME:-~/.belay}`

```text
config.json             non-secret installation/runtime configuration
belay.sqlite            encrypted Local event store
live/codex.ndjson       append-only Codex monitor-mode record stream
live/claude.ndjson      append-only Claude monitor-mode record stream
live/*.cursor.json      importer file identity and byte offset
logs/                   payload-free operational logs
bin/numbat-<checksum>   private checksum-addressed Numbat executable
```

The data-encryption key remains in the OS keychain. Browser and MCP credentials
must not be written to logs.

Belay Local protects against accidental disclosure and untrusted event content;
it is not a tamper-proof boundary against the logged-in OS user. A same-UID
process may replace binaries, hook files, or local state and may suppress or
forge endpoint observations. Release signing and checksum pinning protect the
distribution and normal activation path, not a machine already controlled by
its owner or an attacker running as that owner.

## Acquisition behavior

- Belay invokes Numbat only through its executable interface.
- Historical scans use `numbat scan --agent codex --emit all --output stdout`
  and the equivalent `--agent claude` command.
- Inventory command: `numbat agents --format json`.
- Live installation uses separate monitor-only hook commands and spool files
  for Codex and Claude Code.
- Enforcement is never enabled by Belay Local.
- Hook changes require explicit user intent and preserve upstream backups.
- Scan failure or one malformed artifact does not prevent Local from opening.
- Live import resumes from a durable file cursor and tolerates truncation or
  rotation without duplicating accepted events.

## Local API

The server binds only to an ephemeral port on `127.0.0.1`. A random per-launch
bearer token protects JSON routes.

Required routes:

- `GET /healthz`
- `GET /v1/sessions`
- `GET /v1/sessions/{id}`
- `GET /v1/sessions/{id}/events`
- `GET /v1/findings`
- `GET /v1/stats`

The embedded browser is a client of these routes and never reads SQLite
directly. Event-derived strings render as text, never HTML.

For the Local preview, session-list and session-event responses include
`has_more`, `returned_count`, and the effective bounded `limit`.
`next_cursor` remains `null`; production cursor pagination is still required.
The browser safely expands bounded limits up to 100 sessions and 500 events,
then explicitly discloses when additional rows remain.

Session projections without observed `session.end` terminal evidence report
`incomplete`, never success. This is a projection state and does not change the
canonical event outcome enum.

## MCP

Required read-only tools:

- `list_sessions`
- `get_session`
- `get_session_timeline`
- `query_activity`
- `list_findings`
- `get_stats`

Responses are bounded, structured, schema-versioned, and label event-derived
strings as untrusted observations. No tool can execute a command, write a file,
modify an agent, record a fix, or register recurrence.

Local preview `list_sessions` recalculates `returned_count`, `limit`, and
`has_more` after filtering. It conservatively reports `has_more=true` when its
underlying bounded page may contain unread rows. Cursor paging remains
unavailable in the preview.

## Launch acceptance

1. On a macOS account with Codex and Claude Code history, one Local invocation
   discovers both and renders sessions from both without an account.
2. Re-running the scan inserts no duplicate canonical events.
3. With networking disabled, browser and MCP session reads still work.
4. Explicit hook installation records a new supported agent action without
   blocking or changing the agent action.
5. Killing Belay or Numbat does not block an agent action.
6. Prompt bodies, transcripts, secrets, endpoint identity, raw paths, and
   prohibited evidence do not appear in API, MCP, logs, or database bytes.
7. Every timeline row contains its immutable Belay event ID and source metadata.
8. A malformed/oversized record is quarantined or diagnosed while later valid
   records continue.
9. A clean checkout passes tests, vet, static builds, and a no-network smoke
   test.
10. The release contains the selected Belay license plus Numbat license and
    third-party attribution.

## Release blockers

- Belay edge license selection
- Time-bounded approval for the checksum-verified Numbat research commit, or a
  released upstream tag with schema 0.3.0
- Apple Developer ID credentials for a signed/notarized public macOS package

These block a production-style public package, but not implementation or an
unsigned developer preview.
