# Belay Engine

Belay is endpoint-first observability for AI agents.

This repository is the planned public Belay edge: the wrapper around a pinned,
unmodified Numbat release, the local event store, Belay Local timeline, Local
read-only MCP server, and the optional Belay Teams uploader.

Belay Teams is a separate hosted product. Its closed-source control plane will
live in a separate private repository and consume the public, versioned
contracts published here.

## Product boundaries

### Belay Local

- Free, one machine, and accountless.
- Works without a network connection.
- Sends no product telemetry.
- Stores only minimized, locally redacted normalized events.
- Provides a local timeline and read-only MCP.
- Makes no model calls.

### Belay Teams

- Enabled only through explicit workspace enrollment.
- Uploads minimized, locally redacted normalized events—not raw prompts,
  transcripts, secrets, or fingerprint-only substitutes.
- Adds fleet state, shared timelines, workflow statistics, attended minutes,
  alerts, exports, and hosted APIs.
- Makes no Belay-owned or Belay-orchestrated model calls in V1.

### Explicitly not V1

- Write-capable MCP
- Fix execution or fix recording
- Recurrence registration
- Prevention or blocking guarantees
- Customer instrumentation SDKs
- Cross-customer learning corpora
- Backstop runtime integration

## Source of truth

Product and architecture precedence:

1. Belay BRD v2.2 (internal)
2. Belay HLD v1.1 (internal)
3. Doplex PRFAQ v3 (internal; pending alignment)
4. The landing-page repository, as a visual and acquisition-story reference

The PRFAQ and landing page still require a later copy-alignment pass. They do
not override the BRD or HLD.

## Implementation kickoff

The repository is currently in M0: contracts, dependency validation, local
storage, and Teams ingest foundations.

Start with:

- [`docs/implementation/implementation-plan.md`](docs/implementation/implementation-plan.md)
- [`docs/implementation/numbat-adapter-spike.md`](docs/implementation/numbat-adapter-spike.md)
- [`docs/decisions/0001-stack-and-repository-boundaries.md`](docs/decisions/0001-stack-and-repository-boundaries.md)
- [`docs/storage/local-storage-lifecycle.md`](docs/storage/local-storage-lifecycle.md)
- [`docs/contracts/event-envelope-v1.md`](docs/contracts/event-envelope-v1.md)
- [`docs/contracts/transmitted-fields-v1.md`](docs/contracts/transmitted-fields-v1.md)
- [`docs/contracts/teams-ingest-v1.md`](docs/contracts/teams-ingest-v1.md)
- [`docs/contracts/read-api-v1.md`](docs/contracts/read-api-v1.md)
- [`docs/contracts/mcp-v1.md`](docs/contracts/mcp-v1.md)

The M0 foundation now includes strict Numbat ingestion, encrypted Local
persistence, explicit Local retention primitives, and the Teams ingest
contracts. The Local launch branch adds automatic agent inventory/backfill,
monitor-only live hooks, a loopback browser, and read-only MCP. It remains a
developer preview until licensing, the production Numbat pin, and signed
macOS packaging are resolved.

## Belay Local developer preview

Current launch validation covers Codex and Claude Code. Stock Numbat may report
additional parser-backed agents, but Belay labels those as upstream-supported
until they have Belay end-to-end fixtures.

Prerequisites:

- macOS
- Go 1.27
- A stock Numbat binary built from the recorded research commit
  `f0778c09dc48281aa93a3887d05096c0a1f3f9f7`

Build Belay:

```bash
CGO_ENABLED=0 go build -trimpath -o ./bin/belay ./cmd/belay
```

Build the currently approved research Numbat dependency from its unmodified
checkout:

```bash
git clone https://github.com/perplexityai/numbat.git
git -C numbat checkout f0778c09dc48281aa93a3887d05096c0a1f3f9f7
CGO_ENABLED=0 go -C numbat build -trimpath -o ../bin/numbat ./cmd/numbat
NUMBAT_SHA256="$(shasum -a 256 ./bin/numbat | awk '{print $1}')"
```

Start the offline Local browser and scan discovered Codex/Claude history:

```bash
./bin/belay local \
  --numbat ./bin/numbat \
  --numbat-sha256 "$NUMBAT_SHA256" \
  --numbat-version-marker "f0778c09dc48"
```

The command prints a loopback URL containing an ephemeral launch token. Belay
stores no browser token in the database or logs.

Live monitoring is explicit and monitor-only:

```bash
./bin/belay hooks install
./bin/belay hooks status
```

Belay never passes Numbat enforcement, HTTP delivery, full-content, or reasoning
capture options. Hook installation changes supported agent configuration and
therefore never happens unless the user asks for it.

Configure an agent to run the Local MCP server over stdio:

```bash
./bin/belay mcp
```

The MCP server exposes exactly six read-only tools: `list_sessions`,
`get_session`, `get_session_timeline`, `query_activity`, `list_findings`, and
`get_stats`.

Additional commands:

```bash
./bin/belay agents
./bin/belay scan
./bin/belay doctor
./bin/belay hooks uninstall
```

See [`docs/launch/local-v0-requirements.md`](docs/launch/local-v0-requirements.md)
for the launch contract and known blockers.

## Non-negotiable implementation rules

1. Numbat is pinned and unmodified and is consumed through its versioned NDJSON
   process boundary.
2. Belay never sits in the agent action path.
3. Collection failure must not affect the harness.
4. Disclosure failure is fail-closed: unsafe events are not stored or sent.
5. Local works without an account or hosted dependency.
6. Teams upload starts only after affirmative enrollment.
7. Historical Local data requires separate consent before Teams import.
8. Every timeline row remains traceable to an immutable event ID.
9. Derived metrics carry version, coverage, and confidence.
10. The dashboard, MCP adapters, and integrations share one read contract.
