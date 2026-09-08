# Candidate Contract: Read-Only MCP V1

- **Status:** Candidate; proposed resolution of BRD O6
- **Local:** V1
- **Hosted:** V1.1

## Boundary

MCP is a read adapter over the Belay read contract. It performs no model
inference, remediation, file write, command execution, fix recording, or
recurrence registration.

## Proposed stable tools

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
input error.

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
