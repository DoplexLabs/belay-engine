# Candidate Contract: Belay Read API V1

- **Status:** Local Alpha implemented subset plus future candidate surface
- **Local base:** loopback-only, implementation-defined port
- **Future Teams base:** `/v1`

## Principle

Belay has one intended read contract with Local and future Teams adapters. The
Local Alpha implements only the routes explicitly listed below. Other resources
remain candidate design and must not be represented as available Local Alpha or
Teams functionality.

## Implemented Local Alpha routes

| Route | Local Alpha behavior |
|---|---|
| `GET /healthz` | loopback health response |
| `GET /v1/sessions` | filtered, cursor-paginated local sessions |
| `GET /v1/sessions/{id}` | one local session and metadata-only overview |
| `GET /v1/sessions/{id}/events` | cursor-paginated local timeline |
| `GET /v1/activity` | filtered, cursor-paginated canonical activity |
| `GET /v1/findings` | filtered, cursor-paginated local findings |
| `GET /v1/stats` | global Local summary only |

## Future candidate routes

The following are not implemented by the Local Alpha:

- `GET /v1/machines`
- `GET /v1/workflows`
- `GET /v1/workflows/{id}/stats`
- `GET /v1/alerts`
- `GET /v1/exports/{id}`
- all hosted Teams routes, workspace authorization, and exports

## Common response rules

- JSON only in V1.
- Local Alpha list routes implement opaque cursor pagination with deterministic
  ordering and immutable ingestion snapshots.
- Bounded default and maximum page sizes.
- Explicit `schema_version`, projection/metric versions, and
  coverage/confidence where values are derived.
- `data_through` indicates projection freshness.
- Event-derived strings are untrusted display data.
- Unknown outcomes are never hidden or coerced to success.

List response:

```json
{
  "schema_version": "belay.read.v1",
  "data": [],
  "next_cursor": null,
  "has_more": false,
  "returned_count": 0,
  "limit": 20,
  "data_through": "2026-09-08T18:12:45Z"
}
```

Local session, session-event, activity, and finding lists include:

- `returned_count` is the number of rows in `data`.
- `limit` is the effective bounded request limit.
- Session lists set `filters_applied=true` to confirm that filters were applied
  by the Local read service rather than only by a client.
- `has_more=true` means at least one additional matching row exists in the
  cursor snapshot.
- `next_cursor` is a non-empty opaque string exactly when `has_more=true`;
  otherwise it is `null`.

Cursors are versioned, endpoint-specific, bound to the normalized filter set,
and carry an immutable ingestion snapshot plus the last deterministic sort key.
Clients must not inspect or modify them. Reusing a cursor with different
filters, another endpoint, malformed encoding, or an unsupported version
returns `400 application/problem+json` without reflecting the cursor.

Rows appended after the first page are excluded from that cursor chain,
including late or out-of-order events that would otherwise change a session's
sort position. A fresh request without a cursor starts a new snapshot.

Deterministic ordering:

- Sessions: `ended_at DESC, session_id ASC`
- Session events: `source.sequence ASC, occurred_at ASC, event_id ASC`
- Activity: `occurred_at DESC, source.sequence DESC, event_id DESC`
- Findings: `detected_at DESC, finding_id DESC`

## Local session list

`GET /v1/sessions` accepts:

- `limit` (default 20, maximum 100)
- `cursor`
- `harness` (case-insensitive exact match)
- `outcome`: raw projection value `incomplete`, `succeeded`, `failed`,
  `interrupted`, or `unknown`
- `history`: `historical`, `live`, or `mixed`
- `occurred_after` and `occurred_before`: RFC3339 bounds; sessions whose
  observed event interval overlaps the requested window are returned
- `query`: maximum 128 bytes, case-insensitive substring search over only the
  safe `session_id` and `harness` fields

Session summaries retain the compatibility `historical` boolean and add
`history`. `historical=true` means the session contains at least one
artifact-reconstructed event. `history` truthfully distinguishes sessions
whose stored events are all `historical`, all `live`, or `mixed`.

## Session projection outcomes

Session summaries use projection outcomes. `incomplete` means no
`session.end` terminal evidence was observed and must not be interpreted as
success. Completed projections preserve `succeeded`, `failed`, `interrupted`,
or `unknown` from terminal evidence.

`incomplete` is a session-projection state only. It does not extend the
canonical event outcome enum, which remains `succeeded`, `failed`,
`interrupted`, or `unknown`.

## Local session detail overview

`GET /v1/sessions/{id}` returns the session summary plus a deterministic
`overview` computed only from stored canonical metadata:

```json
{
  "schema_version": "belay.read.v1",
  "data": {
    "session_id": "ses_...",
    "harness": "codex",
    "started_at": "2026-09-08T18:00:00Z",
    "ended_at": "2026-09-08T18:05:00Z",
    "event_count": 12,
    "outcome": "incomplete",
    "historical": true,
    "history": "mixed",
    "overview": {
      "counts": {
        "commands": 2,
        "tool_calls": 3,
        "file_reads": 1,
        "file_writes": 1,
        "file_deletes": 0,
        "network_indicators": 1,
        "permission_events": 2,
        "explicit_failed_events": 1,
        "source_unreported_outcomes": 7,
        "findings": 1
      },
      "salient_resources": [
        {"kind": "file", "name": "src/main.go", "event_count": 2}
      ],
      "salient_resources_truncated": false,
      "observed_coverage": {
        "depths": ["artifact", "tool_call"],
        "confidences": ["high", "medium"]
      },
      "outcome": {
        "value": "incomplete",
        "source": "absence_of_session_end",
        "explanation": "No session.end event was observed; Belay does not infer task success."
      }
    }
  },
  "data_through": "2026-09-08T18:05:01Z"
}
```

Counting rules are deliberately mechanical:

- `commands` counts `command.exec`, not result records.
- `tool_calls` counts `tool.call`, not result records.
- File and network counts correspond to their exact canonical event types.
- `permission_events` counts requested, approved, and denied events.
- Failed/unreported counts use raw canonical event outcomes.
- Findings are linked by stored session key.

Salient resources contain at most 20 distinct canonical resource records,
ordered by event count descending then kind/name ascending. Coverage arrays
contain sorted distinct observed values. No prompt, completion, reasoning,
command output, file content, diff, environment value, URL query, or semantic
task name is returned.

Outcome explanations are fixed display-safe strings:

- `incomplete` is sourced from `absence_of_session_end`.
- Completed projection values are sourced from `session.end`.
- Belay never infers task success, stalls, or semantic task names.

## Local activity and findings

`GET /v1/activity` accepts `limit` (maximum 200), `cursor`,
`occurred_after`, `occurred_before`, `harness`, `resource_kind`, and canonical
event `outcome`. Resource filtering scans the complete cursor snapshot; matches
are not silently omitted because they fall outside an internal candidate
window.

`GET /v1/findings` accepts `limit` (maximum 100), `cursor`, `since`, `severity`,
and optional exact `session_id`. Finding rows retain their cited canonical event
IDs. The cursor is bound to the normalized severity, time, and session filters.
Both endpoints return the truthful list metadata defined above.

`GET /v1/stats` is global-only in Local V1. It accepts no time or workflow
filters. Filtered or workflow statistics must not be advertised until the
underlying projection is implemented.

## Authentication

- Local browser requests use a random per-launch token.
- Local MCP uses stdio only. It opens no network listener and has no loopback
  credential. The configured client spawns the process, and access is bounded by
  the logged-in user's process, filesystem, database, and Keychain permissions.
- Future Teams authentication and workspace authorization are not implemented
  by the Local Alpha.

## Standard filters

Implemented Local filters, where applicable:

- `occurred_after`
- `occurred_before`
- `harness`
- `history`
- `outcome`
- `resource_kind`
- `severity`
- `session_id` for findings

## Errors

Errors use `application/problem+json` with:

- `type`
- `title`
- `status`
- `detail` without payload reflection
- `request_id`

## Acceptance tests

1. A timeline can be reconstructed using documented Local endpoints alone.
2. The Local browser performs no data read outside the implemented routes.
3. Local rejects non-loopback access.
4. Pagination remains stable with late and out-of-order events.
5. Malformed, cross-endpoint, and filter-mismatched cursors fail closed.
6. Resource-kind activity filtering is exhaustive within its cursor snapshot.

Teams shape compatibility and cross-workspace authorization remain future
acceptance requirements, not Local Alpha claims.
