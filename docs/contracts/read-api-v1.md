# Belay Read API V1 Contract

- **Status:** Implemented Local Alpha surface, including Feature 2 Attention
  and issue reads
- **Local base:** loopback-only, implementation-defined port
- **Future Teams base:** `/v1`

## Principle

Belay has one intended read contract with Local and future Teams adapters. The
Local Alpha implements only the routes explicitly listed below. Other resources
must not be represented as available Local Alpha or Teams functionality.

Feature 2 exposes the internal snapshot-queryable issue repository through the
loopback HTTP API and browser Attention Inbox. This does not add MCP issue
tools: MCP remains exactly six tools until Feature 5.

## Implemented Local Alpha routes

| Route | Local Alpha behavior |
|---|---|
| `GET /healthz` | loopback health response |
| `GET /v1/sessions` | filtered, cursor-paginated local sessions |
| `GET /v1/sessions/{id}` | one local session and metadata-only overview |
| `GET /v1/sessions/{id}/events` | cursor-paginated local timeline |
| `GET /v1/sessions/{id}/events/lookup` | bounded exact cited-event lookup within one session |
| `GET /v1/activity` | filtered, cursor-paginated canonical activity |
| `GET /v1/findings` | filtered, cursor-paginated local findings |
| `GET /v1/issues` | filtered, cursor-paginated deterministic issue summaries |
| `GET /v1/issues/{id}/occurrences` | issue detail and cursor-paginated exact matching sessions |
| `GET /v1/stats` | global Local summary only |

## Other future candidate routes

The following are not implemented by the Local Alpha:

- `GET /v1/machines`
- `GET /v1/workflows`
- `GET /v1/workflows/{id}/stats`
- `GET /v1/alerts`
- `GET /v1/exports/{id}`
- all hosted Teams routes, workspace authorization, and exports

## Common response rules

- JSON only in V1.
- Implemented Local Alpha list routes use opaque cursor pagination with
  deterministic ordering and immutable ingestion snapshots.
- Issue routes use a separate immutable issue-projection generation snapshot.
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

## P0 issue and Attention foundation

The internal issue repository groups retained detector occurrences only when
they have the same opaque, versioned fingerprint. A fingerprint represents an
exact deterministic detector signature within a compatible project scope. It
does not establish semantic similarity, shared intent, or a common root cause.

The implemented repository and HTTP presentation provide:

- stable public `issue_id` and exact `fingerprint_id` values;
- issue summaries aggregated from occurrence revisions visible at one
  projection generation;
- exact matching-session occurrences with immutable cited event IDs;
- origin separation between Belay detectors and immutable Numbat findings;
- per-session analysis state and list-level completeness;
- revisioned projection snapshots that remain stable while reconciliation
  processes new or late evidence;
- default separation of stable issues, experimental signals, and verification
  evidence gaps;
- bounded exact cited-event lookup within one selected session.

This is derived, rebuildable state. Canonical events and upstream Numbat
findings remain the evidence source. Unknown outcomes remain unknown, event
strings remain untrusted observations, and issue records never claim root
cause, correctness, safety, intent, or successful remediation.

### Issue summary shape

The implemented issue HTTP routes use this logical summary. Planned Feature 5
MCP issue tools may reuse it without changing the current six-tool MCP surface:

```json
{
  "issue_id": "iss_...",
  "fingerprint_id": "ifp_...",
  "fingerprint_version": "1",
  "origin": "belay",
  "detector_id": "explicit_command_failure",
  "detector_version": "1",
  "category": "command_failure",
  "title_code": "issue.explicit_command_failure",
  "severity": "medium",
  "confidence": "high",
  "scope_quality": "resolved",
  "first_observed_at": "2026-09-08T17:00:00Z",
  "last_observed_at": "2026-09-08T18:00:00Z",
  "occurrence_count": 3,
  "session_count": 2,
  "harnesses": ["claude", "codex"],
  "analysis_status": "current",
  "evidence_complete": true,
  "retained_history_only": false,
  "experimental": false
}
```

Identifiers are opaque. `issue_id` is the stable address used by detail
consumers. `fingerprint_id` may be used only for exact matching; clients must
not label matching fingerprints as semantic similarity or shared root cause.
Catalog codes are fixed display keys, not generated narratives.

At a projection snapshot:

- counts include all visible occurrences in the issue group;
- session count is the number of distinct visible session IDs;
- first/last timestamps cover retained Local history only;
- severity is the highest visible fixed severity rank;
- confidence is the lowest visible confidence;
- analysis status is the least-current visible status in this order:
  `failed`, `pending`, `truncated`, `current`;
- evidence is complete only when every visible occurrence is complete;
- harnesses are sorted and distinct;
- repeated means at least two distinct sessions;
- unscoped or conflicting sessions never establish cross-session recurrence.

Filters select issue groups. Returned counts and aggregate fields continue to
describe the complete visible group at the snapshot rather than only the
occurrences that matched a filter.

### Issue occurrence shape

An issue occurrence represents one exact fingerprint in one session:

```json
{
  "occurrence_id": "occ_...",
  "issue_id": "iss_...",
  "fingerprint_id": "ifp_...",
  "fingerprint_version": "1",
  "session_id": "ses_...",
  "harness": "codex",
  "origin": "belay",
  "provenance": {
    "detector_id": "explicit_command_failure",
    "detector_version": "1",
    "fingerprint_version": "1",
    "projection_version": "belay.issue.v1"
  },
  "category": "command_failure",
  "title_code": "issue.explicit_command_failure",
  "first_observed_at": "2026-09-08T17:58:00Z",
  "last_observed_at": "2026-09-08T18:00:00Z",
  "severity": "medium",
  "confidence": "high",
  "scope_quality": "resolved",
  "analysis_status": "current",
  "analysis_generation": 42,
  "evidence_complete": true,
  "retained_history_only": false,
  "experimental": false,
  "evidence": {
    "cited_event_ids": ["01890f2e-6d4b-7c8a-9b0c-123456789abc"],
    "dimensions": ["cmd_..."]
  }
}
```

For `origin=numbat`, `origin_record_id` references the immutable source finding.
It is omitted for Belay-origin occurrences. `evidence.cited_event_ids` refer to
canonical events retrievable through the exact session event lookup route.
Evidence values and opaque dimensions returned with an occurrence are
untrusted observations.

## Implemented issue HTTP contract

### `GET /v1/issues`

Accepted query parameters:

- `limit`: default 20, maximum 100;
- `cursor`: opaque issue-list cursor;
- `severity`: exact fixed rank `info`, `low`, `medium`, `high`, or `critical`;
- `category`: exact fixed catalog category code;
- `harness`: case-insensitive exact harness match;
- `origin`: `belay` or `numbat`;
- `analysis_status`: `current`, `pending`, `failed`, or `truncated`;
- `observed_after`: RFC3339 lower bound selecting groups with a visible
  occurrence at or after the bound;
- `recurrence`: `single` or `repeated`;
- `session_id`: exact Belay session identifier;
- `fingerprint_id`: exact opaque fingerprint identifier;
- `attention_kind`: `issue`, `evidence_gap`, or `all`; default `issue`;
- `experimental`: `stable`, `include`, or `only`; default `stable`.

Ordering is severity descending (`critical`, `high`, `medium`, `low`, `info`),
repeated before single within a severity, `last_observed_at DESC`, then
`issue_id ASC`. All normalized filters are bound into the cursor.
The default response excludes experimental signals and evidence gaps.
Verification evidence gaps are requested separately with
`attention_kind=evidence_gap`; they do not inflate the default issue count.

Response:

```json
{
  "schema_version": "belay.read.v1",
  "projection_version": "belay.issue.v1",
  "data": [],
  "analysis": {
    "current_sessions": 120,
    "pending_sessions": 2,
    "failed_sessions": 1,
    "truncated_sessions": 0,
    "unscoped_sessions": 8,
    "analysis_through": "2026-09-08T18:05:01Z",
    "complete": false
  },
  "view_cursor": "opaque-rowless-view-cursor",
  "next_cursor": null,
  "has_more": false,
  "returned_count": 0,
  "limit": 20
}
```

`analysis.complete` is true only when no retained session is pending, failed, or
truncated at the response snapshot. Unscoped sessions may be fully analyzed but
cannot support cross-session recurrence. When `complete=false`, an empty result
means only that no issue is available from the completed portion. Consumers
must identify incomplete coverage and must not say that no issues exist.

`view_cursor` carries the list's immutable issue-projection snapshot without a
row position. A client passes it to the initial detail request so list and
detail remain on the same logical view.

### `GET /v1/issues/{id}/occurrences`

Accepted query parameters:

- `limit`: default 20, maximum 100;
- `cursor`: opaque occurrence cursor bound to the issue ID;
- `view_cursor`: rowless cursor from `GET /v1/issues`, accepted only for the
  initial detail request.

`cursor` and `view_cursor` are mutually exclusive. Exact issue lookup includes
experimental and evidence-gap rows because selecting an opaque issue ID is
explicit intent.

The response contains the issue summary at the cursor snapshot and occurrence
rows ordered by `last_observed_at DESC, occurrence_id ASC`, followed by the
common list metadata. The route returns `404 application/problem+json` when the
issue does not exist at a fresh snapshot. It returns a cursor error, rather
than `404`, when a supplied cursor is malformed, expired, or belongs to another
issue.

```json
{
  "schema_version": "belay.read.v1",
  "projection_version": "belay.issue.v1",
  "data": {
    "issue": {},
    "occurrences": []
  },
  "next_cursor": null,
  "has_more": false,
  "returned_count": 0,
  "limit": 20
}
```

### Issue cursor semantics

Issue cursors carry an immutable projection generation, normalized filters,
last deterministic sort key, and issued-at time. They expire after 15 minutes.
Projection revision history is retained for at least one hour, but clients must
honor the shorter cursor lifetime.

- A fresh request without a cursor reads the current projection generation.
- Reconciliation after page one cannot add, remove, or reorder rows in that
  cursor chain.
- A malformed, cross-route, issue-mismatched, or filter-mismatched cursor
  returns `400 application/problem+json` without reflecting cursor contents.
- An expired cursor, or one older than the oldest retained projection
  generation, returns `410 application/problem+json` with fixed type
  `belay.local/cursor-expired`.
- Clients restart pagination without a cursor after expiration.

### `GET /v1/sessions/{id}/events/lookup`

This route performs one bounded exact lookup of cited canonical events within
the selected session. It accepts only repeated `event_id` query parameters:

- one to 50 values;
- lowercase canonical UUIDv7 values;
- duplicates removed while preserving first-request order for missing-ID
  reporting;
- no cursor, free-form query, or other query parameter.

Returned events use canonical timeline ordering. An event belonging to another
session is reported as missing rather than returned. Missing IDs are ordinary
data, not `404`.

```json
{
  "schema_version": "belay.read.v1",
  "data": [],
  "requested_count": 2,
  "found_count": 1,
  "missing_count": 1,
  "missing_event_ids": ["01890f2e-6d4b-7c8a-9b0c-123456789abc"],
  "data_through": "2026-09-08T18:05:01Z"
}
```

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
- issue filters documented under `GET /v1/issues`, including
  `attention_kind` and `experimental`

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
7. Issue pagination remains stable while reconciliation advances.
8. Attention defaults exclude experimental signals and keep Evidence gaps
   separate.
9. Incomplete analysis is reported truthfully and never converted into a
   complete empty-state claim.
10. Matching sessions use exact fingerprint equality, never semantic or
    shared-root-cause language.
11. Exact event lookup remains bounded, session-constrained, and rejects
    unrelated query parameters.

Teams shape compatibility and cross-workspace authorization remain future
acceptance requirements, not Local Alpha claims.
