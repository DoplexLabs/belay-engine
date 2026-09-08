# Belay Read API V1 Contract

- **Status:** Implemented Local Alpha surface, including Feature 2 Attention,
  P0-03 browser-only fix-attempt actions, and P0-04 recurrence-monitoring reads
- **Local base:** loopback-only, implementation-defined port
- **Future Teams base:** `/v1`

## Principle

Belay has one intended read contract with Local and future Teams adapters. The
Local Alpha implements only the routes explicitly listed below. Other resources
must not be represented as available Local Alpha or Teams functionality.

Feature 2 exposes the internal snapshot-queryable issue repository through the
loopback HTTP API and browser Attention Inbox. This does not add MCP issue
tools: MCP remains exactly six tools until Feature 5.

P0-03 adds four Local-only routes for explicit browser fix-attempt declarations.
They are not future Teams read-contract claims and do not make MCP write-capable.

P0-04 adds three Local-only, read-only monitoring routes over exact compatible
fingerprint observations after a recorded attempt. They do not claim semantic
similarity, resolution, prevention, or fix success and are not exposed to MCP.

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
| `GET /v1/issues/{id}/fix-eligibility` | snapshot-bound eligibility and signed browser action token |
| `GET /v1/issues/{id}/fixes` | durable, cursor-paginated fix-attempt history |
| `POST /v1/issues/{id}/fixes` | append one browser-confirmed external fix-attempt declaration |
| `POST /v1/issues/{id}/fixes/{annotation_id}/retractions` | append one fixed-reason retraction |
| `GET /v1/fix-monitoring` | grouped, cursor-paginated post-attempt monitoring |
| `GET /v1/issues/{id}/fix-monitoring` | one issue's durable attempt monitoring/history |
| `GET /v1/issues/{id}/fixes/{annotation_id}/recurrences` | exact post-baseline observations for one attempt |
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

Each parameter is accepted at most once, unknown or empty parameters return
`400`, and `limit` must be an integer from 1 through 100. `cursor` and
`view_cursor` are mutually exclusive; a continuation request contains only
`cursor`. Exact issue lookup includes experimental and evidence-gap rows
because selecting an opaque issue ID is explicit intent.

The response contains the issue summary at the cursor snapshot and occurrence
rows ordered by `last_observed_at DESC, occurrence_id ASC`, followed by the
common list metadata. The route returns `404 application/problem+json` when the
issue does not exist at a fresh snapshot. It returns a cursor error, rather
than `404`, when a supplied cursor is malformed, expired, or belongs to another
issue. Every successful response also returns a rowless `view_cursor` for the
same exact issue snapshot; Local browser fix eligibility may consume that
cursor without first locating the issue in a paginated issue list.

```json
{
  "schema_version": "belay.read.v1",
  "projection_version": "belay.issue.v1",
  "data": {
    "issue": {},
    "occurrences": []
  },
  "view_cursor": "opaque-rowless-view-cursor",
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

## P0-03 Local browser fix-attempt contract

These routes record only a developer declaration that an external change was
attempted. Belay does not execute the change, inspect a diff, mark the issue
resolved, suppress future detections, or verify an outcome.

All responses use `schema_version=belay.fix.v1`. Fix actions are available only
through explicitly configured Local HTTP. They are not part of MCP or the CLI.

### Fixed catalogs

`change_catalog_version` is `fix-change.v1`. `change_kind` must be exactly one
of:

- `code_change`;
- `configuration_change`;
- `dependency_change`;
- `permission_change`;
- `environment_change`;
- `agent_instruction`;
- `project_rule`;
- `monitor_hook`;
- `other`.

Retraction `reason` must be exactly one of:

- `recorded_by_mistake`;
- `superseded`;
- `other`.

No route accepts a free-text note, command, path, diff, prompt, output, rule or
hook body, environment value, URL, client timestamp, client-selected annotation
ID, occurrence ID, fingerprint, state, or outcome.

### `GET /v1/issues/{id}/fix-eligibility`

Accepts exactly one non-empty `view_cursor` from the current Attention issue
view. The cursor is structurally decoded and freshness-checked, then the server
evaluates the complete issue aggregate and latest visible anchor in one read
transaction.

Eligibility requires:

- a visible stable issue at the supplied snapshot;
- aggregate analysis status `current`;
- non-experimental origin data;
- a category other than `evidence_gap`;
- `resolved` or `lexical` scope quality.

No visible issue rows return `404`. Aggregate status covers every visible
occurrence, so any non-current visible occurrence returns
`analysis_not_current`.

Eligible response:

```json
{
  "schema_version": "belay.fix.v1",
  "data": {
    "eligible": true,
    "reason": "eligible",
    "action_token": "<signed opaque token>",
    "expires_at": "2026-09-08T18:15:00Z",
    "change_catalog_version": "fix-change.v1"
  }
}
```

Ineligible current issues return `200` with `eligible=false`,
`action_token=null`, `expires_at=null`, and one fixed reason:

- `analysis_not_current`;
- `experimental_signal`;
- `evidence_gap`;
- `scope_unavailable`.

The action token authenticates its structure, issue ID, immutable issue
projection snapshot, issued-at time, and expiry. Invalid cursor syntax returns
`400`; an expired or compacted issue view returns `410
belay.local/cursor-expired`.

### `POST /v1/issues/{id}/fixes`

Required headers:

```text
Authorization: Bearer <per-launch Local token>
Content-Type: application/json
Idempotency-Key: <canonical lowercase UUIDv4>
X-Belay-Intent: record-fix-attempt.v1
Origin: http://<exact numeric loopback listener address and port>
```

The request `Host` must equal the actual listener address and port. If
`Sec-Fetch-Site` is present, it must be `same-origin`. Missing, `null`,
cross-origin, DNS-name, wrong-port, or duplicate security headers are rejected.
Forwarded host and protocol headers are ignored. CORS is not enabled.

The body is limited to 1 KiB, must use identity encoding, must contain exactly
one JSON object, and rejects unknown fields, duplicate fields, or trailing JSON:

```json
{
  "action_token": "<signed opaque token>",
  "change_kind": "code_change"
}
```

The server authenticates token structure and MAC and verifies the signed issue
against the route before durable idempotency lookup. For a new declaration it
then validates token expiry and snapshot freshness, derives aggregate
eligibility, chooses the latest visible occurrence, and captures its exact
revision, generation, timestamps, fingerprint, session, detector provenance,
and cited event IDs in the same transaction.

First creation returns `201` and `replayed=false`. An identical request using
the same idempotency key returns the original row with `200` and
`replayed=true`, including after action-token expiry. Reusing that key with
different canonical intent returns `409 belay.local/idempotency-conflict`.

Create/replay response:

```json
{
  "schema_version": "belay.fix.v1",
  "data": {
    "annotation_id": "fxa_...",
    "issue_id": "iss_...",
    "anchor_revision_id": "ior_...",
    "anchor_occurrence_id": "occ_...",
    "anchor_session_id": "ses_...",
    "fingerprint_id": "ifp_...",
    "fingerprint_version": "1",
    "origin": "belay",
    "detector_id": "explicit_command_failure",
    "detector_version": "1",
    "scope_quality": "resolved",
    "issue_snapshot_generation": 42,
    "anchor_analysis_generation": 41,
    "anchor_first_observed_at": "2026-09-08T17:00:00Z",
    "anchor_last_observed_at": "2026-09-08T17:02:00Z",
    "change_kind": "code_change",
    "change_catalog_version": "fix-change.v1",
    "recorded_via": "local_ui",
    "recorded_at": "2026-09-08T18:00:00Z",
    "monitor_from": "2026-09-08T18:00:00Z",
    "evidence_currently_retained": "available",
    "state": "active",
    "retraction_reason": null,
    "retracted_at": null
  },
  "replayed": false
}
```

Active response DTOs encode `retraction_reason` and `retracted_at` explicitly as
JSON `null`.

### `GET /v1/issues/{id}/fixes`

Accepts only:

- `limit`: default 20, maximum 100;
- `cursor`: opaque fix-history cursor bound to the issue ID.

A fresh request captures independent annotation and retraction high-water marks.
Rows are ordered by `recorded_at DESC, annotation_id DESC`. New annotations and
new retractions after page one are excluded from the existing cursor chain.
Fix-history cursors do not expire under ordinary retention.

Response:

```json
{
  "schema_version": "belay.fix.v1",
  "data": [],
  "next_cursor": null,
  "has_more": false,
  "returned_count": 0,
  "limit": 20,
  "evidence_evaluated_at": "2026-09-08T18:05:00Z"
}
```

Each row uses the complete annotation DTO shown above. `state` is `active` or
`retracted`. Retracted rows contain their fixed `retraction_reason` and
`retracted_at`; active rows contain explicit nulls.

`evidence_currently_retained` is evaluated at page-read time:

- `available`: every originally cited event remains;
- `partial`: some cited events remain;
- `pruned`: no originally cited event remains;
- `unknown`: the annotation had no baseline citations.

A syntactically valid issue ID with no annotation rows returns an empty `200`
even if the issue projection no longer contains that issue. History remains
readable after issue disappearance and Local restart.

### `POST /v1/issues/{id}/fixes/{annotation_id}/retractions`

Uses the same bearer, exact listener `Host`/`Origin`, fetch-site, media type,
encoding, 1 KiB body, strict JSON, and UUIDv4 idempotency requirements as
creation, with:

```text
X-Belay-Intent: retract-fix-attempt.v1
```

Body:

```json
{"reason":"recorded_by_mistake"}
```

The original annotation is never changed or deleted. First append returns `201`;
an identical retry returns `200` and `replayed=true`. Reusing the key with
different canonical content returns `409 belay.local/idempotency-conflict`.
Trying to append another retraction with a different key returns `409
belay.local/already-retracted`.

Response:

```json
{
  "schema_version": "belay.fix.v1",
  "data": {
    "retraction_id": "fxr_...",
    "annotation_id": "fxa_...",
    "issue_id": "iss_...",
    "reason": "recorded_by_mistake",
    "recorded_via": "local_ui",
    "retracted_at": "2026-09-08T18:10:00Z"
  },
  "replayed": false
}
```

### Fix error contract

Error responses are `application/problem+json`, contain a server-generated
`request_id`, and never reflect the bearer token, action token, idempotency key,
cursor, issue ID, annotation ID, or request body.

| Condition | Status/type |
|---|---|
| Missing or invalid bearer | `401 about:blank` |
| Missing/mismatched listener Origin or Host, bad fetch-site or intent | `403 belay.local/write-forbidden` |
| Unsupported media type or content encoding | `415 belay.local/unsupported-media-type` |
| Body over 1 KiB | `413 belay.local/request-too-large` |
| Malformed query/header/JSON/enum/ID/key/token | `400 about:blank` |
| Expired/compacted action snapshot for a new declaration | `410 belay.local/cursor-expired` |
| Issue or annotation not found | `404 about:blank` |
| Current issue is ineligible | `409 belay.local/ineligible-fix-annotation` |
| Idempotency key reused for different content | `409 belay.local/idempotency-conflict` |
| Annotation already retracted through another request | `409 belay.local/already-retracted` |
| Storage failure | `500 about:blank` |

### Persistence, retention, and privacy

Annotations and retractions are append-only Local state in the Keychain-backed
store. Their minimized records contain opaque identifiers, fixed catalog
values, timestamps, and request fingerprints; no free-text or evidence payload
is accepted. Opaque identities and request fingerprints are derived with
store-specific, domain-separated keys; raw idempotency keys are never persisted
or logged.

Ordinary event/finding/issue retention excludes annotation and retraction rows.
Event pruning cascades only annotation citation sidecars, which may change
read-time evidence status without deleting the declaration. History, create
replay, and retraction replay survive Local process restart when the same Local
database and Keychain key are used. A full Local database reset removes them
with the rest of Local state.

## P0-04 exact recurrence-monitoring contract

These authenticated, loopback-only reads report deterministic exact compatible
fingerprint observations after a fix-attempt's server-recorded `monitor_from`.
A matching observation is attention evidence, not proof that a fix failed. No
later match is not proof that a fix worked.
`historical_matching_evidence_count` aggregates qualifying post-baseline
observations across active and retracted attempts; it does not describe
pre-attempt evidence.

All responses use `schema_version=belay.fix-monitoring.v1`. Lists default to 20
rows and are bounded to 100. Arrays are non-null. Nullable fields are emitted as
explicit JSON `null`.

### Fixed monitoring catalogs

`fix_recurrence_state` is one of:

- `matching_evidence_observed`;
- `monitoring_incomplete`;
- `awaiting_later_evidence`;
- `no_later_match_observed`;
- `comparison_unavailable`;
- `retracted`.

`future_comparison_unavailable_reason` is null or one of:

- `scope_unavailable`;
- `source_positive_only`;
- `capability_unavailable`;
- `baseline_time_unavailable`;
- `fingerprint_version_unsupported`.

Future unknown recurrence-state values are exposed as neutral `unknown` so the
browser can render “Monitoring status unavailable” without inferring a known
comparison state. Unknown comparison-unavailability reasons fail closed to
`capability_unavailable`.

Evidence-retention state is `available`, `partial`, `pruned`, or `unknown`.
Coverage contains `comparable_current`, `comparable_pending`,
`comparable_failed`, `comparable_truncated`, nullable `analysis_through`, and
`complete`. `analysis_complete` equals `coverage.complete`.
`count_is_lower_bound=true` only for a positive observation count with
incomplete coverage.

### `GET /v1/fix-monitoring`

The top-level list returns one grouped row per issue. By default it includes
issues with active attempts; `include_retracted=true` also permits all-retracted
history rows.

Initial query parameters, each accepted at most once:

- `state`: exact recurrence-state catalog value;
- `change_kind`: exact `fix-change.v1` value;
- `severity`: `info`, `low`, `medium`, `high`, or `critical`;
- `harness`: exact normalized harness, maximum 128 bytes;
- `recorded_after`: RFC3339 instant;
- `issue_id`: exact canonical issue ID;
- `include_retracted`: exact lowercase `true` or `false`;
- `limit`: 1–100.

A continuation request contains only `cursor`. Grouping selects the driving
attempt before filters. Rows are ordered by state rank, known severity rank,
`COALESCE(last_recurrence_observed_at, recorded_at) DESC`, then `issue_id ASC`.
Equal positions remain deterministic.

Response:

```json
{
  "schema_version": "belay.fix-monitoring.v1",
  "data": [{
    "issue_id": "iss_...",
    "annotation_id": "fxa_...",
    "title_code": "explicit_command_failure",
    "severity": "high",
    "change_kind": "code_change",
    "recorded_at": "2026-09-08T18:00:00Z",
    "monitor_from": "2026-09-08T18:00:00Z",
    "fix_recurrence_state": "matching_evidence_observed",
    "fix_recurrence_count": 2,
    "same_anchor_session_observation_count": 1,
    "other_session_observation_count": 1,
    "historical_matching_evidence_count": 2,
    "active_attempt_count": 2,
    "observed_attempt_count": 1,
    "analysis_complete": false,
    "count_is_lower_bound": true,
    "future_comparison_available": true,
    "future_comparison_unavailable_reason": null,
    "last_recurrence_observed_at": "2026-09-08T19:14:00Z",
    "coverage": {
      "comparable_current": 3,
      "comparable_pending": 1,
      "comparable_failed": 0,
      "comparable_truncated": 0,
      "analysis_through": "2026-09-08T19:16:00Z",
      "complete": false
    }
  }],
  "returned_count": 1,
  "limit": 20,
  "has_more": false,
  "next_cursor": null,
  "monitoring_view_cursor": "<opaque>",
  "evidence_evaluated_at": "2026-09-08T19:17:00Z"
}
```

All-retracted grouped rows have zero active/observed-attempt counts, state
`retracted`, empty current coverage with null `analysis_through`, and may retain
historical observation counts.

### `GET /v1/issues/{id}/fix-monitoring`

This route paginates attempts ordered by `recorded_at DESC, annotation_id DESC`.
An initial read accepts optional `limit` and either:

- no cursor, creating a fresh monitoring snapshot; or
- exactly one `view_cursor` transferred from the top-level
  `monitoring_view_cursor`.

Continuation accepts only `cursor`. `cursor` and `view_cursor` are mutually
exclusive.

Response:

```json
{
  "schema_version": "belay.fix-monitoring.v1",
  "issue_id": "iss_...",
  "current_issue_available": false,
  "current_issue": null,
  "data": [{
    "annotation_id": "fxa_...",
    "issue_id": "iss_...",
    "subject": {
      "title_code": "explicit_command_failure",
      "severity": "high",
      "confidence": "high",
      "anchor_harness": "codex",
      "origin": "belay",
      "detector_id": "explicit_command_failure",
      "detector_version": "1",
      "fingerprint_version": "1"
    },
    "change_kind": "code_change",
    "recorded_at": "2026-09-08T18:00:00Z",
    "monitor_from": "2026-09-08T18:00:00Z",
    "state": "active",
    "retraction_reason": null,
    "retracted_at": null,
    "fix_recurrence_state": "matching_evidence_observed",
    "fix_recurrence_count": 1,
    "same_anchor_session_observation_count": 0,
    "other_session_observation_count": 1,
    "historical_matching_evidence_count": 1,
    "analysis_complete": true,
    "count_is_lower_bound": false,
    "future_comparison_available": true,
    "future_comparison_unavailable_reason": null,
    "last_recurrence_observed_at": "2026-09-08T19:14:00Z",
    "coverage": {
      "comparable_current": 1,
      "comparable_pending": 0,
      "comparable_failed": 0,
      "comparable_truncated": 0,
      "analysis_through": "2026-09-08T19:16:00Z",
      "complete": true
    },
    "anchor_evidence_currently_retained": "available",
    "recurrence_evidence": {
      "available": 1,
      "partial": 0,
      "pruned": 0,
      "unknown": 0
    },
    "observation_view_cursor": "<opaque>"
  }],
  "returned_count": 1,
  "limit": 20,
  "has_more": false,
  "next_cursor": null,
  "monitoring_view_cursor": "<opaque>",
  "evidence_evaluated_at": "2026-09-08T19:17:00Z"
}
```

`current_issue_available=false` requires `current_issue=null`; history remains
readable after the issue leaves the current projection. When available,
`current_issue` is the bounded issue-summary DTO. Nullable subject fields are
exactly `title_code`, `severity`, `confidence`, and `anchor_harness`. Active
attempts encode `retraction_reason` and `retracted_at` as null.

### `GET /v1/issues/{id}/fixes/{annotation_id}/recurrences`

The first observation request requires exactly one
`observation_view_cursor` from its attempt row and may include `limit`.
Continuation requests contain only `cursor`. Rows are ordered by
`first_qualifying_event_at DESC, recurrence_id DESC`.

Response:

```json
{
  "schema_version": "belay.fix-monitoring.v1",
  "issue_id": "iss_...",
  "annotation_id": "fxa_...",
  "data": [{
    "recurrence_id": "fxo_...",
    "occurrence_id": "occ_...",
    "session_id": "ses_...",
    "fingerprint_version": "1",
    "origin": "belay",
    "detector_id": "explicit_command_failure",
    "detector_version": "1",
    "first_qualifying_event_at": "2026-09-08T19:13:00Z",
    "last_qualifying_event_at": "2026-09-08T19:14:00Z",
    "qualifying_citation_count": 2,
    "retained_event_ids": [
      "019921c0-7abc-7def-8abc-0123456789ab"
    ],
    "retained_event_count": 1,
    "missing_event_count": 1,
    "evidence_complete": true,
    "evidence_truncated": false,
    "evidence_currently_retained": "partial",
    "same_session_as_anchor": false,
    "observed_at": "2026-09-08T19:15:00Z"
  }],
  "returned_count": 1,
  "limit": 20,
  "has_more": false,
  "next_cursor": null,
  "evidence_evaluated_at": "2026-09-08T19:17:00Z"
}
```

`retained_event_ids` contains at most 50 canonical lowercase UUIDv7 event IDs
for the row's session. The existing session-constrained lookup route may fetch
them without scanning a broad timeline. `qualifying_citation_count` is the
immutable original count. `evidence_complete` records detector completeness at
observation time. `evidence_truncated=true` means more than 50 retained
citations exist.

For unknown evidence the exact DTO is:

- `retained_event_ids=[]`;
- `retained_event_count=null`;
- `missing_event_count=null`;
- `evidence_truncated=null`;
- `evidence_currently_retained="unknown"`.

### Monitoring snapshots, readiness, and errors

Monitoring uses dedicated opaque cursor kinds for the top list, list view,
issue attempts, per-attempt observation view, and observation pages. Cursors
bind the endpoint, normalized filters, route IDs, original page size, row
position, issued-at time, and one snapshot containing issue projection, event,
retention, annotation, retraction, recurrence-job, job-event, and observation
high-water marks.

The snapshot lifetime is 15 minutes. A changed retention generation also
expires the chain. View cursors are rowless snapshot transfers; page cursors
carry deterministic row position. Monitoring cursors are not accepted by
legacy session/activity/finding/issue routes, and legacy cursors are not
accepted here.

Schema migration completes before Local starts. Historical recurrence catch-up
runs after the HTTP server starts. During `catching_up` or `failed`, only the
three monitoring routes fail closed; all older Local reads and fix writes
remain available.

Validation/error precedence is authentication, path syntax, query syntax,
cursor syntax/binding, readiness, expiry/retention validity, resource
existence, repository read, then encoding.

| Condition | Status/type |
|---|---|
| Missing/invalid bearer | `401 about:blank` |
| Invalid/repeated/unknown query, ID, limit, filter, or cursor envelope | `400 about:blank` |
| Catch-up active | `503 belay.local/monitoring-catchup-in-progress` |
| Catch-up failed awaiting recovery | `503 belay.local/monitoring-catchup-failed` |
| Expired/compacted/retention-invalid cursor | `410 belay.local/cursor-expired` |
| Missing issue/history, missing annotation, or route-binding mismatch | `404 about:blank` |
| Repository/encoding failure | `500 about:blank` |

Problem details and server-generated request IDs never reflect request or
stored values. Cancellation leaves catch-up durable and retryable rather than
recording a false failure. Local restart resumes incomplete catch-up. Once
ready, a failed individual recurrence job contributes incomplete attempt
coverage and retries independently instead of disabling all monitoring.

## Authentication

- Local browser requests use a random per-launch token.
- Local fix writes additionally require exact same-origin intent bound to the
  actual numeric loopback listener, strict JSON, a route-specific intent header,
  and a canonical UUIDv4 idempotency key.
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
- monitoring filters documented under `GET /v1/fix-monitoring`, including
  exact recurrence state, change kind, recorded-after, issue ID, and
  include-retracted

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
12. Fix writes fail closed without the actual listener-bound Origin/Host and
    explicit browser intent.
13. Fix creation/retraction are append-only, idempotent, payload-free, and
    survive Local restart and ordinary retention.
14. Fix recording and exact recurrence monitoring remain Local HTTP/browser
    capabilities; MCP still exposes exactly six read-only tools.
15. Monitoring cursors bind every documented high-water, route ID, normalized
    filter, page size, and ordering position for 15 minutes.
16. Catch-up 503 affects only monitoring routes; cancellation/restart converges
    without exposing partial history as complete.
17. Durable observations and attempt history survive restart and issue
    disappearance; retention changes expire old cursors and truthfully degrade
    evidence on fresh reads.
18. Matching evidence is never described as fix failure, and no-match,
    incomplete, unavailable, or unknown evidence is never described as success.

Teams shape compatibility and cross-workspace authorization remain future
acceptance requirements, not Local Alpha claims.
