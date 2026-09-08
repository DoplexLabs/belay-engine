# Belay Engine

Belay Local is private, endpoint-first observability for an individual developer
using AI coding agents. It reconstructs minimized local activity into one
timeline and exposes the same evidence through a loopback browser and read-only
MCP. The browser may also record a narrowly scoped, fixed-schema declaration
that the developer attempted an external fix; Belay does not execute, describe,
or verify that change.

This repository contains the Apache-2.0-licensed Belay edge. Belay Teams is a
separate product and is not included in this Local alpha.

## Belay Local Developer Alpha

The prepared version is `0.0.1-alpha.1`. It is an **unsigned, unnotarized,
Apple Silicon-only Developer Alpha**, not a published release or production
installer.

Alpha scope:

- macOS on Apple Silicon (`darwin/arm64`)
- Codex and Claude Code only
- One machine, no account, and no hosted Belay dependency
- No product telemetry or Belay model calls
- Encrypted local SQLite payloads backed by macOS Keychain
- Historical scanning and explicit monitor-only live hooks
- Loopback-only browser with a random per-launch token
- Attention Inbox with deterministic issues and exact matching sessions
- Browser-only, append-only fix-attempt declarations with no free-text field
- Exactly six read-only MCP tools

Intel macOS builds remain possible for engineering validation, but Intel is not
part of the alpha support claim until it passes the clean-machine checklist.
Linux, Windows, Teams, enforcement, remediation, and write-capable MCP are out
of scope.

The Attention Inbox turns the private deterministic issue projection into a
browser triage surface. It groups only exact compatible fingerprints, links
matching sessions and bounded cited events, and never claims semantic
similarity or shared root cause. Experimental signals are hidden by default,
verification evidence gaps appear separately, and incomplete analysis is
reported rather than converted into a complete "no issues" claim.

The alpha packages the exact approved, unmodified Numbat research commit:

```text
f0778c09dc48281aa93a3887d05096c0a1f3f9f7
```

This is a checksum-verified research exception, not a stable upstream release
tag.

## Build the alpha locally

Go 1.27 is required to build from source. From a clean checkout on Apple
Silicon:

```bash
make verify
make preview ALPHA_VERSION=0.0.1-alpha.1 ALPHA_ARCH=arm64
```

Run the complete non-publishing automated readiness path:

```bash
make alpha-readiness ALPHA_VERSION=0.0.1-alpha.1
```

That target verifies source and release-surface checks, builds the arm64
archive, verifies its checksum, runs the packaged smoke test, and prints the
remaining manual gates. It never signs, notarizes, tags, publishes, releases,
deploys, or modifies real Belay/harness state.

The expected files are:

```text
dist/belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz
dist/belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz.sha256
```

No artifact is published merely by running these commands.

## Verify and run

Obtain the archive and checksum through an explicitly authorized channel. From
the directory containing both files:

```bash
shasum -a 256 -c \
  belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz.sha256

tar -xzf belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz
cd belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64

./bin/belay quickstart
```

Running `quickstart` is explicit consent for Belay to create private state under
`${BELAY_HOME:-~/.belay}`, verify its packaged sibling Numbat, install reversible
monitor-only hooks in detected Codex and Claude Code configurations, scan
supported history, start Local, print its tokenized loopback URL, and attempt to
open the dashboard. If the browser cannot be opened, the printed URL remains
usable. The hooks observe supported activity but never block agent actions.

For a lower-side-effect first run that scans history and starts Local without
installing hooks or opening a browser, use:

```bash
./bin/belay local
```

Hooks installed by `quickstart` can be inspected or removed:

```bash
./bin/belay hooks status
./bin/belay hooks uninstall
```

The alpha goal is a useful historical timeline within 15 minutes, but that
remains a measured clean-machine gate rather than a guarantee until external
alpha evidence is recorded.

## Read-only MCP

Run the stdio server:

```bash
./bin/belay mcp
```

The tools are:

- `list_sessions`
- `get_session`
- `get_session_timeline`
- `query_activity`
- `list_findings`
- `get_stats`

The MCP server exposes exactly the six tools above. Planned `list_issues` and
`get_issue` tools remain deferred until Feature 5; the implemented Attention
Inbox and issue HTTP routes do not implicitly expose them through MCP.

MCP results are structured and marked as untrusted observations. The alpha
`get_stats` tool provides global Local summary counts only; time-window and
workflow-filtered statistics are not implemented.

Session, timeline, activity, and finding lists use bounded, filter-bound opaque
cursor pagination over a stable ingestion snapshot. Session filters cover
harness, projection outcome, historical/live/mixed capture, RFC3339 overlap
windows, and bounded search over session ID and harness. Resource-kind activity
queries scan the complete cursor snapshot rather than a fixed candidate window.

The browser Attention Inbox uses a separate revisioned projection snapshot with
explicit current, pending, failed, truncated, and unscoped analysis coverage.
Issue detail shows exact matching sessions and retrieves cited evidence through
a bounded, session-constrained event lookup. Eligible current stable issues can
record an append-only external fix-attempt declaration using one fixed
`fix-change.v1` category. The declaration is not a resolution claim and does not
execute remediation. Recurrence measurement remains future P0-04 work.

Exact Codex and Claude Code configuration examples are in
[`docs/launch/developer-preview.md`](docs/launch/developer-preview.md).

## Privacy and evidence limitations

- Belay Local sends no product telemetry and requires no Doplex service.
- Prompt bodies, transcripts, file contents, raw endpoint identity, and raw
  evidence paths are excluded from canonical events.
- Minimized envelope/index fields remain plaintext in SQLite; canonical event
  JSON and finding citations are encrypted.
- Command summaries may retain the executable name and bounded option names.
  File resources retain a project-relative path or basename, and network
  resources retain scheme plus host.
- Event observations are operational evidence, not tamper-proof forensic
  evidence. A process running as the same macOS user can alter or suppress local
  state.
- Unknown source outcomes remain unknown. Sessions without observed terminal
  evidence are labeled `Incomplete`, never successful.
- Fix-attempt declarations store no note, command, path, diff, prompt, output,
  environment value, or URL. They survive ordinary evidence retention and Local
  restarts, and remain until the Local database is reset. Mistakes are preserved
  and corrected through append-only fixed-reason retractions.

See
[`docs/storage/local-storage-lifecycle.md`](docs/storage/local-storage-lifecycle.md)
for the storage and key lifecycle.

## Packaging integrity

The packaging script:

- requires a clean Belay checkout for a distributable artifact and refuses a
  dirty or wrong Numbat checkout;
- verifies exact upstream license hashes;
- builds Belay and Numbat with `CGO_ENABLED=0`, `-trimpath`, and no VCS build
  stamping;
- builds Numbat first and embeds its exact binary SHA-256 plus version marker in
  Belay so packaged first use needs no manual pin flags;
- includes both binaries, Belay's Apache-2.0 license, and Numbat's exact license
  and third-party attribution;
- creates internal `SHA256SUMS` plus an archive checksum;
- emits deterministic archive metadata from `SOURCE_DATE_EPOCH`.

Numbat attribution is vendored under [`licenses/numbat`](licenses/numbat).
`BELAY_ALPHA_ALLOW_DIRTY=1` exists only for local validation of uncommitted
release-surface changes. Any resulting `belay_dirty=true` artifact is never
distributable, regardless of which automated checks pass.

## QA and community

- Clean-machine acceptance:
  [`docs/launch/clean-machine-alpha-qa.md`](docs/launch/clean-machine-alpha-qa.md)
- Detailed alpha guide:
  [`docs/launch/developer-preview.md`](docs/launch/developer-preview.md)
- Security reporting: [`SECURITY.md`](SECURITY.md)
- Contributions: [`CONTRIBUTING.md`](CONTRIBUTING.md)
- Support and feedback: [`SUPPORT.md`](SUPPORT.md)
- Agent-readable overview: [`llms.txt`](llms.txt)

The normal CI workflow validates code and release-surface checks. The separate
Developer Alpha workflow is manual-only and may upload short-lived workflow
artifacts; it contains no release or publishing step.
