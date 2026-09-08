# Candidate Contract: Teams Ingest API V1

- **Status:** Candidate
- **Contract ID:** `belay.ingest.v1`
- **Endpoint:** `POST /v1/events`
- **Authentication:** machine signature over TLS

## Authoritative schemas

The machine-readable Draft 2020-12 contracts are:

| Message | Schema ID | Repository path |
|---|---|---|
| Request | `https://schemas.doplex.ai/belay/ingest/v1/request.schema.json` | `contracts/ingest/v1/request.schema.json` |
| Acknowledgement | `https://schemas.doplex.ai/belay/ingest/v1/acknowledgement.schema.json` | `contracts/ingest/v1/acknowledgement.schema.json` |
| Problem details | `https://schemas.doplex.ai/belay/ingest/v1/problem.schema.json` | `contracts/ingest/v1/problem.schema.json` |

The request schema references the canonical event schema by its stable ID,
`https://schemas.doplex.ai/belay/event/v1/event.schema.json`. Contract-owned
objects are closed: fields not named by these schemas are rejected, and there
are no arbitrary extension maps.

## Request

Headers:

```text
Content-Type: application/json
Content-Encoding: gzip (optional)
Belay-Credential-ID: cred_...
Belay-Timestamp: 2026-09-08T18:12:45Z
Belay-Nonce: base64url-random
Belay-Content-SHA256: lowercase-hex
Belay-Signature: base64url-ed25519-signature
```

Signature input is a canonical byte sequence containing method, path,
credential ID, timestamp, nonce, and content digest. The exact canonicalization
must be frozen with cross-language fixtures before implementation.

Valid body:

```json
{
  "schema_version": "belay.ingest.v1",
  "batch_id": "0199d7a5-6dc1-7a2b-8c3d-55c16b5d10f2",
  "client_watermark": 812,
  "events": [
    {
      "schema_version": "belay.event.v1",
      "event_id": "0199d7a5-6bc0-7b9e-8c7c-55c16b5d10f2",
      "installation_id": "inst_01K4BELAY",
      "occurred_at": "2026-09-08T18:12:43.123456Z",
      "observed_at": "2026-09-08T18:12:43.201004Z",
      "source": {
        "engine": "numbat",
        "engine_version": "research-commit",
        "schema_version": "0.3.0",
        "record_type": "event",
        "record_id": "upstream-event-42",
        "kind": "artifact",
        "agent": "codex",
        "adapter_version": "numbat-0.3.0/v1",
        "deduplication_key": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "sequence": 42
      },
      "session": {
        "key": "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      },
      "observation": {
        "type": "command.result",
        "actor": "tool",
        "action": "command",
        "outcome": "failed",
        "exit_code": 1,
        "summary": "npm --",
        "resource": {
          "kind": "command",
          "name": "npm"
        },
        "details": {
          "tool_call_id": "call-42",
          "tags": ["test.contract"]
        }
      },
      "coverage": {
        "depth": "tool_call",
        "confidence": "high"
      },
      "redaction": {
        "policy_version": "belay.redaction.v1",
        "fields_removed": 0,
        "secrets_removed": 0
      },
      "historical": {
        "is_historical": false
      }
    }
  ]
}
```

`batch_id` is a lowercase UUIDv7. `client_watermark` is a nonnegative integer.
Each request contains between 1 and 500 canonical events.

The body must not contain authoritative workspace or machine identity. The
server resolves both from the credential.

## Candidate limits

- Compressed request: 1 MiB
- Decompressed request: 8 MiB
- Events per batch: 500
- Event size: 16 KiB
- Timestamp skew: 5 minutes for live requests
- Nonce replay window: 10 minutes

These are safe starting values, not launch promises.

## Validation order

1. Reject unsupported encoding or oversized compressed body.
2. Resolve active credential.
3. Verify timestamp, nonce, digest, and signature.
4. Enforce decompressed-size and event-count limits.
5. Validate batch and event schemas.
6. Enforce transmitted-field policy.
7. Insert batch, new events, and projection job in one transaction.
8. Return acknowledgement only after durable commit.

## Responses

### `202 Accepted`

New events were durably committed.

```json
{
  "request_id": "req-01K4BELAY8R7Y6T5S4Q3P2N1M0",
  "batch_id": "0199d7a5-6dc1-7a2b-8c3d-55c16b5d10f2",
  "accepted": 1,
  "duplicates": 0,
  "acknowledged_watermark": 812
}
```

### `200 OK`

The batch was a valid duplicate replay and no new event was inserted. It uses
the same acknowledgement schema; `accepted` is zero and `duplicates` is the
number of already-present events. For every acknowledgement, `accepted` plus
`duplicates` equals the request event count and is therefore at most 500.

### Errors

- `400` malformed JSON or invalid request framing
- `401` unknown, invalid, expired, or revoked credential/signature
- `403` credential is valid but not entitled to ingest
- `409` batch ID reused with a different content digest
- `413` compressed/decompressed/event limits exceeded
- `415` unsupported content encoding/type
- `422` bounded batch, event, or transmitted-field validation failure
- `429` rate limit
- `500` internal server error
- `503` temporary ingest dependency failure; the client may retry the same body

Errors use `application/problem+json`, include a request ID, and never echo
event payloads. The bounded response follows the current API's RFC 9457-style
shape:

```json
{
  "type": "urn:belay:problem:batch-digest-conflict",
  "title": "Batch conflict",
  "status": 409,
  "detail": "The batch ID was previously used with different content.",
  "request_id": "req-01K4BELAY8R7Y6T5S4Q3P2N1M0"
}
```

## Idempotency

- `batch_id` is unique per credential with a stored content digest.
- Event uniqueness is `(workspace_id, machine_id, event_id)`.
- Reusing a batch ID with different content is a conflict and security audit
  event.

## Retry semantics

An ambiguous or retryable attempt preserves the exact request body and content
digest. In particular, it preserves `batch_id`, every `event_id`, event
ordering, and `client_watermark`.

Each attempt generates a fresh `Belay-Timestamp`, a never-before-used
`Belay-Nonce`, and a fresh `Belay-Signature` over the new timestamp and nonce
plus the unchanged content digest. A client must never reuse a prior timestamp,
nonce, or signature.

## Acceptance tests

1. Duplicate replay inserts zero duplicate rows.
2. Cross-workspace identity supplied in a body cannot override credential scope.
3. Invalid signature, digest, nonce replay, timestamp, and revoked credential
   each fail before payload persistence.
4. A database failure yields no acknowledgement and the same batch can retry.
5. Logs, traces, and error responses contain no event body.
6. Request, acknowledgement, and problem examples validate against their
   published schemas with the canonical event schema registered by absolute ID.
