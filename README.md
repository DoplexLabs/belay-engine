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
- Exact post-attempt recurrence monitoring with bounded retained evidence
- Exactly nine read-only MCP tools, including issue discovery and exact cited-event lookup

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
monitor-only hooks for detected Codex and Claude Code installations, safely
inspect supported MCP configuration, scan supported history, start Local,
print its tokenized loopback URL, and attempt to open the dashboard. Claude MCP
registration is automatic only when Claude's existing status is safely
understood. Normal quickstart does not add an absent Codex MCP entry. If the
browser cannot be opened, the printed URL remains usable. Hooks observe
supported activity but never block agent actions. MCP registration launches
only `belay mcp`; it adds no write-capable tool.

To include Codex MCP registration in the same command:

```bash
./bin/belay quickstart --allow-codex-mcp-add
```

This flag permits `codex mcp add` only after Belay strictly verifies that the
`belay` entry is absent. It explicitly accepts the Codex CLI's non-atomic
duplicate-name behavior. Belay never invokes that add operation against an
existing entry; foreign or unverifiable entries are preserved and never
overwritten or removed.

To keep hook installation but skip MCP configuration:

```bash
./bin/belay quickstart --no-mcp
```

For a lower-side-effect first run that scans history and starts Local without
installing hooks, changing MCP configuration, or opening a browser, use:

```bash
./bin/belay local
```

Inspect the exact MCP ownership state and hook state:

```bash
./bin/belay mcp-config status
./bin/belay hooks status
```

MCP and hooks are removed independently:

```bash
./bin/belay mcp-config uninstall
./bin/belay hooks uninstall
```

`mcp-config uninstall` removes only an exact current or previously verified
Belay-owned `belay` entry. It preserves foreign or unverifiable entries and does
not remove hooks, Local history, the Keychain-backed data key, or the extracted
package. If the archive moves, quickstart leaves a prior Codex entry unchanged.
Belay does not automatically update, migrate, replace, or remove it. Inspect it
with `mcp-config status`; after verifying it is Belay-owned, explicitly run
`mcp-config uninstall`, verify absence, and then rerun
`quickstart --allow-codex-mcp-add`.

The alpha goal is a useful historical timeline within 15 minutes, but that
remains a measured clean-machine gate rather than a guarantee until external
alpha evidence is recorded.

## Read-only MCP

Normal `quickstart` may register Claude automatically when its status is safely
understood. Codex registration requires the explicit opt-in shown above. The
same operations are available directly:

```bash
./bin/belay mcp-config install --allow-codex-mcp-add
./bin/belay mcp-config status
```

`mcp-config install` may automatically configure Claude when its status is
safely understood. The `--allow-codex-mcp-add` flag is required before it may
add an absent Codex entry and carries the same explicit acceptance of Codex's
non-atomic duplicate-name behavior. A foreign or unverifiable `belay` entry is
never overwritten or removed. Standalone install may initialize private Belay
configuration and directories to preserve a stable ownership ID; it does not
create or open the Local database or create Keychain material. Registration is
local, requires no Doplex service, account login, or paid service, and failures
do not prevent the Local browser from opening.

For Codex, install never mutates a current, recognized prior, foreign,
unverifiable, or unavailable entry. A current exact entry is left unchanged.
For a recognized prior Codex archive entry, verify ownership with
`mcp-config status`, explicitly run `mcp-config uninstall`, verify that the
entry is absent, and then rerun
`mcp-config install --allow-codex-mcp-add`.

Agent clients launch `./bin/belay mcp` over stdio on demand. The tools are:

- `list_sessions`
- `get_session`
- `get_session_timeline`
- `query_activity`
- `list_findings`
- `get_stats`
- `list_issues`
- `get_issue`
- `lookup_session_events`

The issue-evidence loop is deliberately structured: call `list_issues`, check
its normalized selection and analysis coverage, pass its `view_cursor` to
`get_issue`, then hydrate only needed cited IDs with
`lookup_session_events`. Matching sessions share an exact compatible
fingerprint; they are not claimed to be semantically similar or to share a
root cause.

MCP results are structured and marked as untrusted observations. Fixed issue
catalog statements describe what deterministic evidence was reported and its
caveats; they are not generated diagnosis or remediation advice. The
configured calling agent may reason over the evidence. The alpha `get_stats`
tool provides global Local summary counts only; time-window and
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
execute remediation.

Belay can subsequently report whether the same exact compatible fingerprint was
observed after the declaration's server-recorded monitoring baseline. Monitoring
is grouped by issue and attempt, distinguishes same-anchor-session from
other-session observations, and reports incomplete or unavailable comparison
coverage explicitly. A matching observation is attention evidence, not proof
that a fix failed; no match is not proof that a fix worked. Recurrence
monitoring reports only post-attempt evidence observed after the recorded
attempt baseline.
Monitoring is available through authenticated Local HTTP/browser routes only.
MCP remains exactly the nine read-only tools listed above and receives no fix
recording, fix history, or recurrence-monitoring capability.

Exact Codex and Claude Code configuration examples are in
[`docs/launch/developer-preview.md`](docs/launch/developer-preview.md).

## Privacy and evidence limitations

- Belay Local sends no product telemetry and requires no Doplex service.
- Belay Local and its MCP server make no product-network request. A configured
  MCP client or remotely hosted model may process or transmit tool results
  according to that product's privacy policy and the user's configuration.
- Prompt bodies, transcripts, file contents, raw endpoint identity, and raw
  evidence paths are excluded from canonical events.
- Minimized envelope/index fields remain plaintext in SQLite; canonical event
  JSON and finding citations are encrypted.
- Command summaries may retain the executable name and bounded option names.
  File resources retain a project-relative path or basename, and network
  resources retain scheme plus host. Minimized evidence may also include
  tool names and model/provider labels.
- Event-derived evidence is untrusted data. It must not be treated as an
  instruction or, by itself, as authorization to run a command or use another
  tool. Belay does not claim prompt-injection immunity for the configured
  client/model.
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
