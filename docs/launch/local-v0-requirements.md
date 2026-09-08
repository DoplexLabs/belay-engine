# Belay Local Developer Alpha Requirements

Status: implementation contract for the Apple Silicon individual-developer
alpha slice.

## Product promise

Belay Local gives one developer a private, offline view of activity across the
AI agent harnesses present on their machine. It reconstructs supported history,
continues collecting supported live activity after explicit hook installation,
and exposes the same read-only evidence through a local browser and MCP.

Belay Local has no account, hosted dependency, product telemetry, model
inference, write-capable MCP tool, or Teams requirement.

The approved P0 scope now includes an internal deterministic issue repository:
exact, opaque fingerprints group retained occurrences across matching sessions,
with analysis completeness and snapshot-stable reads. This foundation does not
make an attention inbox, issue HTTP route, or issue MCP tool available by
itself. Those presentation adapters are the next feature.

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

### Launch-validated platform

- Apple Silicon macOS (`darwin/arm64`)

Intel macOS remains an engineering build target, not an alpha support claim,
until the clean-machine checklist passes on Intel hardware.

### Deferred

- Belay Teams enrollment, upload, API, UI, billing, or deployment
- Write-capable MCP, remediation, or automatic fix execution
- Belay-hosted model inference
- Enforcement or blocking mode
- Windows packaging
- Linux packaging beyond reproducible source builds

Explicit local fix annotation and recurrence measurement are approved later P0
features, not current Local Alpha capabilities. They must not be advertised as
implemented until their separate contracts and presentation paths ship.

## User commands

The launch CLI must converge on these stable workflows:

```text
belay quickstart
belay local
belay local --install-hooks
belay scan
belay agents
belay hooks status|install|uninstall
belay mcp
belay doctor
```

`belay quickstart` is the packaged one-command onboarding path. Invoking it is
explicit consent to initialize private Local state, verify the packaged sibling
Numbat using the checksum and version marker embedded at build time, install
reversible monitor-only hooks for detected Codex and Claude Code installations,
scan supported history, start Local, print the loopback URL, and attempt to open
the dashboard. Browser-open failure is non-fatal because the URL remains
printed.

`belay local` initializes Local state if necessary, performs a historical scan,
imports any new live records, starts the loopback API, and prints the local URL.
It is the lower-side-effect path: it does not open a browser and never edits an
agent configuration unless `--install-hooks` was explicitly provided.

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
- `GET /v1/activity`
- `GET /v1/findings`
- `GET /v1/stats`

The approved next presentation feature adds `GET /v1/issues` and
`GET /v1/issues/{id}/occurrences`. These routes are not required or implemented
by the current Local Alpha. The internal issue repository must not be treated
as evidence that either route exists.

The embedded browser is a client of these routes and never reads SQLite
directly. Event-derived strings render as text, never HTML.

Event rows show prominent outcome badges only when the source explicitly
reports `succeeded`, `failed`, or `interrupted`. When the source does not report
an event outcome, the browser preserves the canonical `unknown` value as
subdued `Outcome · Not reported by source` metadata instead of presenting every
event as a prominent unknown status. Session outcome badges remain unchanged.

Session, session-event, activity, and finding responses include `has_more`,
`next_cursor`, `returned_count`, and the effective bounded `limit`. A non-empty
opaque `next_cursor` is returned exactly when another matching row exists in the
stable ingestion snapshot. Cursors are endpoint-specific and bound to normalized
filters; malformed, cross-endpoint, or filter-mismatched cursors fail closed.

Session filters include harness, raw projection outcome, historical/live/mixed
capture, RFC3339 overlap windows, and bounded search over session ID and harness.
Activity resource-kind filters scan the complete cursor snapshot. Finding
filters include time, severity, and optional exact session ID.

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

The approved next presentation feature adds the read-only `list_issues` and
`get_issue` contracts. They are not part of the current six-tool Local Alpha
server.

Responses are bounded, structured, schema-versioned, and label event-derived
strings as untrusted observations. No tool can execute a command, write a file,
modify an agent, record a fix, or register recurrence.

The MCP list tools use the same server-side filters, deterministic order,
stable-snapshot cursor semantics, limits, and completeness rules as the Local
read API. They do not fetch broad pages and filter them inside MCP.

## P0 issue intelligence foundation

The implemented foundation is an internal, rebuildable projection over retained
canonical events and immutable Numbat findings. It:

- derives private, store-keyed project scopes and exact command signatures
  before raw values are discarded;
- emits conservative deterministic occurrences from a fixed detector catalog;
- groups only exact compatible fingerprints and calls their sessions
  "matching sessions," never semantically similar sessions;
- preserves bounded cited event IDs and detector/catalog provenance;
- maintains revisioned issue and session-analysis state for stable read
  snapshots;
- reports current, pending, failed, truncated, and unscoped analysis coverage.

The foundation does not infer root cause, task intent, correctness, safety,
stalls, or successful completion. Unknown outcomes remain unknown. Unscoped or
conflicting sessions may support session-local attention but never
cross-session recurrence.

The future attention inbox, issue HTTP routes, and `list_issues`/`get_issue` MCP
tools must:

- show list-level analysis completeness and avoid a complete "no issues" claim
  while analysis is pending, failed, or truncated;
- preserve exact matching semantics and expose cited evidence;
- use fixed catalog language and label event-derived values as untrusted;
- paginate over an immutable issue-projection generation;
- expire issue cursors after 15 minutes and require a fresh read rather than
  silently weakening snapshot stability;
- remain read-only and provide no remediation or fix-execution action.

## Launch acceptance

1. On a macOS account with Codex and Claude Code history, one packaged
   `belay quickstart` invocation needs no manual Numbat path or pin flags,
   discovers both harnesses, installs monitor-only hooks, and renders sessions
   from both without an account.
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
11. Every distributable archive is built from a clean checkout and records
    `belay_dirty=false`; dirty validation artifacts are never distributed.

## Alpha and production gates

- Belay is licensed under Apache-2.0; license selection is complete.
- The checksum-verified Numbat research commit is an explicit Developer Alpha
  exception. A released upstream tag with schema 0.3.0, or a renewed exception,
  remains a production gate.
- Apple Developer ID signing and notarization remain production gates, not
  requirements for the explicitly unsigned Developer Alpha.
- Repository visibility, artifact publication, naming clearance, and external
  tester authorization remain human-owned launch gates.

The objective clean-machine evidence is recorded in
`docs/launch/clean-machine-alpha-qa.md`.
