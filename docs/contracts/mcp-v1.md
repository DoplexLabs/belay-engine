# Read-Only MCP V1 Contract

- **Status:** Six implemented Local Alpha tools plus two approved,
  unimplemented P0 issue tools
- **Local:** V1
- **Hosted:** V1.1

## Boundary

MCP is a read adapter over the Belay read contract. It performs no model
inference, remediation, file write, command execution, fix recording, or
recurrence registration.

The internal P0 issue repository does not automatically expose MCP
capabilities. `list_issues` and `get_issue` are frozen contracts for the next
presentation feature and are not available in the current Local Alpha.

## Implemented Local Alpha tools

### `list_sessions`

Lists bounded session summaries.

Inputs:

- `limit` (default 20, maximum 100)
- `cursor` (optional)
- `since` (compatibility alias for `occurred_after`)
- `occurred_after` and `occurred_before` (optional RFC3339 overlap window)
- `harness` (optional)
- `outcome` (optional raw projection value)
- `history` (`historical`, `live`, or `mixed`)
- `query` (maximum 128 bytes; session ID and harness only)

The tool uses the same server-side filters, deterministic ordering, immutable
snapshot, cursor validation, and completeness semantics as
`GET /v1/sessions`. It does not fetch a broad page and filter it in MCP.

### `get_session`

Returns one session summary, source, coverage, confidence, and metric versions.

Inputs:

- `session_id`

### `get_session_timeline`

Returns a bounded ordered event page for a session.

Inputs:

- `session_id`
- `cursor` (optional)
- `limit` (default 100, maximum 500)

### `query_activity`

Queries activity by time, action/resource class, harness, and outcome. It does
not accept arbitrary SQL or regex over unbounded payload text.

Inputs:

- `occurred_after`
- `occurred_before`
- `harness` (optional)
- `resource_kind` (optional)
- `outcome` (optional)
- `cursor` (optional)
- `limit` (default 50, maximum 200)

Resource-kind matches are exhaustive within the cursor snapshot; an internal
candidate window must not silently omit older matches.

### `list_findings`

Lists local detection matches or Teams alerts with supporting event IDs.

Inputs:

- `since` (optional)
- `severity` (optional)
- `session_id` (optional exact Belay session identifier)
- `cursor` (optional)
- `limit` (default 20, maximum 100)

The cursor is bound to the normalized time, severity, and session filters.

### `get_stats`

Returns versioned global Local summary metrics.

Inputs:

- none

Local V1 does not advertise time or workflow filters for `get_stats`. Those
inputs may be added only when the corresponding projections are implemented.

## Approved next-feature tools — not implemented

### `list_issues`

Lists bounded issue summaries from one immutable issue-projection generation.
Fingerprint matches are exact deterministic matches within compatible project
scope; they are never described as semantic similarity or shared root cause.

Inputs:

- `limit` (default 20, maximum 100)
- `cursor` (optional opaque issue-list cursor)
- `severity` (optional `info`, `low`, `medium`, `high`, or `critical`)
- `category` (optional exact fixed catalog category code)
- `harness` (optional case-insensitive exact match)
- `origin` (optional `belay` or `numbat`)
- `analysis_status` (optional `current`, `pending`, `failed`, or `truncated`)
- `observed_after` (optional RFC3339 lower bound)
- `recurrence` (optional `single` or `repeated`)
- `session_id` (optional exact Belay session identifier)
- `fingerprint_id` (optional exact opaque fingerprint identifier)

Ordering and filter aggregation are identical to future
`GET /v1/issues`: severity descending from `critical` through `info`, repeated
before single, `last_observed_at DESC`, then `issue_id ASC`. Filters select
groups while summary counts describe the complete visible group at the
snapshot.

Output:

```json
{
  "schema_version": "belay.read.v1",
  "projection_version": "belay.issue.v1",
  "issues": [],
  "analysis": {
    "current_sessions": 120,
    "pending_sessions": 2,
    "failed_sessions": 1,
    "truncated_sessions": 0,
    "unscoped_sessions": 8,
    "analysis_through": "2026-09-08T18:05:01Z",
    "complete": false
  },
  "returned_count": 0,
  "limit": 20,
  "has_more": false,
  "next_cursor": null
}
```

When `analysis.complete=false`, the tool must not turn an empty `issues` array
into a claim that no issues exist. It reports that no issues are available from
the completed portion and includes the incomplete coverage counts.

### `get_issue`

Returns one issue summary and a bounded page of exact matching-session
occurrences.

Inputs:

- `issue_id` (required opaque public issue identifier)
- `limit` (default 20, maximum 100)
- `cursor` (optional opaque occurrence cursor bound to `issue_id`)

Output:

```json
{
  "schema_version": "belay.read.v1",
  "projection_version": "belay.issue.v1",
  "issue": {},
  "occurrences": [],
  "returned_count": 0,
  "limit": 20,
  "has_more": false,
  "next_cursor": null
}
```

Occurrences are ordered by `last_observed_at DESC, occurrence_id ASC`. They
include session and harness identity, observed interval, origin and immutable
origin-record reference when applicable, analysis generation/status,
confidence, evidence completeness, and an `evidence` object containing bounded
cited canonical event IDs and opaque dimensions. Event-derived evidence is
returned only as untrusted observations.

### Issue MCP cursor and error contract

Issue MCP cursors use the same 15-minute immutable projection snapshot,
filter-binding, issue-binding, ordering, and expiration semantics as the future
issue HTTP routes.

- malformed, cross-tool, issue-mismatched, or filter-mismatched cursors return a
  fixed `invalid_cursor` input error;
- expired cursors or snapshots older than retained projection history return a
  fixed `cursor_expired` input error instructing the client to restart without
  a cursor;
- an unknown issue on a fresh request returns fixed `issue_not_found`;
- errors never reflect cursor or evidence contents.

Neither tool records a fix, registers monitoring, executes remediation, writes
files, runs commands, or modifies agents.

## Response safety

Every tool response:

- Uses structured fields rather than narrative diagnosis.
- Labels event-derived strings as untrusted observations.
- Includes schema/metric versions.
- Includes pagination/truncation state.
- Applies response-size limits.
- Never interprets event content as instructions.

List tools return `next_cursor` exactly when `has_more=true`. Cursors are opaque
and filter-bound; malformed or mismatched cursors fail with a payload-free
input error. Implemented tools use ingestion snapshots. The two future issue
tools use the separately revisioned issue-projection snapshot described above.

## Explicitly absent

- `record_fix`
- `recurrence_since`
- Any `write_*`, `execute_*`, `apply_*`, or `remediate_*` tool
- Arbitrary filesystem or shell access

## Prompt-injection fixtures

Fixtures must include event summaries containing:

- Fake tool instructions
- Requests to reveal secrets
- Markdown links and images
- Terminal escape sequences
- HTML/script fragments
- Oversized nested content

The MCP server returns them as escaped/untrusted data and never changes tool
behavior.
