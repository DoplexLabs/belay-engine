# Candidate Contract: Belay Read API V1

- **Status:** Candidate
- **Local base:** loopback-only, implementation-defined port
- **Teams base:** `/v1`

## Principle

Belay has one read contract with two adapters. Local implements the applicable
one-machine subset. Teams implements the workspace-scoped hosted surface.
Dashboards and MCP adapters use these APIs; neither receives a private data
side door.

## Resources

| Resource | Local | Teams |
|---|---:|---:|
| `GET /machines` | current machine only | workspace fleet |
| `GET /sessions` | yes | yes |
| `GET /sessions/{id}` | yes | yes |
| `GET /sessions/{id}/events` | yes | yes |
| `GET /workflows` | local grouping | workspace grouping |
| `GET /workflows/{id}/stats` | local only | workspace |
| `GET /alerts` | local findings | workspace alerts |
| `GET /stats` | local summary | workspace summary |
| `GET /exports/{id}` | no | yes |

## Common response rules

- JSON only in V1.
- Production cursor pagination with deterministic order remains required.
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

The Local developer preview adds `has_more`, `returned_count`, and `limit` to
session-list and session-event responses:

- `returned_count` is the number of rows in `data`.
- `limit` is the effective bounded request limit.
- `has_more=true` means additional rows exist or completeness cannot be proven
  after bounded filtering.
- `next_cursor` is explicitly `null` because cursor paging is not implemented
  in the Local preview.

The Local browser currently expands bounded limits and uses one-row look-ahead
to expose truncation. It does not provide production cursor pagination. Local
MCP recalculates this metadata after applying `list_sessions` filters and keeps
`has_more=true` when the underlying bounded page reports more rows.

## Session projection outcomes

Session summaries use projection outcomes. `incomplete` means no
`session.end` terminal evidence was observed and must not be interpreted as
success. Completed projections preserve `succeeded`, `failed`, `interrupted`,
or `unknown` from terminal evidence.

`incomplete` is a session-projection state only. It does not extend the
canonical event outcome enum, which remains `succeeded`, `failed`,
`interrupted`, or `unknown`.

## Authentication

- Local browser requests use a random per-launch token.
- Local MCP uses a separately scoped loopback credential.
- Teams uses workspace-bound user/API bearer tokens stored as slow hashes.
- Teams workspace identity comes from the authenticated principal, not a query
  or body field.

## Standard filters

Where applicable:

- `occurred_after`
- `occurred_before`
- `harness`
- `machine_id`
- `workflow_id`
- `outcome`
- `alert_type`

## Errors

Errors use `application/problem+json` with:

- `type`
- `title`
- `status`
- `detail` without payload reflection
- `request_id`

## Acceptance tests

1. Local and Teams return the same session/event fixture shapes.
2. A timeline can be reconstructed using documented endpoints alone.
3. The dashboard performs no data read outside this contract.
4. Local rejects non-loopback access.
5. Teams cross-workspace reads fail closed.
6. Pagination remains stable with late and out-of-order events.
