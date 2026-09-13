# Belay Engine

Belay Local is private, endpoint-first observability for an individual developer
using AI coding agents. It combines minimized canonical activity with
secret-scrubbed transcript content in an encrypted on-device store, then
exposes bounded evidence through a loopback browser and local stdio MCP. Its
governed workflows can record narrowly scoped fixes, Mission Pack acceptance,
and experience-learning decisions; Belay does not execute a command or write a
proposed change to a project.

This repository contains the MIT-licensed Belay edge. Belay Teams is a
separate product and is not included in this Local alpha.

## Belay Local Developer Alpha

The prepared version is `0.0.1-alpha.5`. This founder-led external alpha is
unsigned, unnotarized, and Apple Silicon-only. Publication remains gated on the
full test suite, checksum verification, packaged smoke testing, and explicit
human authorization. A later broadly advertised release remains gated on Apple
Developer ID signing and notarization.

### Install

To install and begin onboarding in one command:

```bash
curl -fsSL https://getbelay.vercel.app/install | bash
```

The short installer resolves to the exact versioned GitHub prerelease in
`DoplexLabs/belay-engine`. It accepts only the expected Apple Silicon archive,
verifies both external and internal checksums, and rejects dirty builds. The
alpha bootstrap explicitly permits the current unsigned package; testers should
review that limitation before running it. Re-running the command upgrades the
installation. Uninstall preserves encrypted local history:

```bash
curl -fsSL https://getbelay.vercel.app/install | bash -s -- --uninstall
```

Alpha scope:

- macOS on Apple Silicon (`darwin/arm64`)
- Codex and Claude Code only
- One machine, no account, and no hosted Belay dependency
- No product telemetry or Belay model calls
- Encrypted local SQLite payloads backed by macOS Keychain
- Historical scanning and explicit monitor-only live hooks
- Loopback-only browser with a random per-launch token
- Attention Inbox with deterministic issues and exact matching sessions
- Bounded fix proposals and append-only application records with no arbitrary
  project-file write
- Governed local MCP tools for evidence, issues, fixes, Mission Packs, and
  experience learning

Intel macOS builds remain possible for engineering validation, but Intel is not
part of the alpha support claim until it passes the clean-machine checklist.
Linux, Windows, Teams, enforcement, command execution, arbitrary MCP writes,
and automatic remediation are out of scope.

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

## New developer handoff

Start with the current code and contracts, not historical design assumptions.
Belay Local now deliberately retains secret-scrubbed full transcript content in
its encrypted on-device store. Data minimization moves to a future Teams upload
boundary. The current product direction is documented in
[`docs/product/belay-product-bets-and-alpha-recommendation-2026-09-09.md`](docs/product/belay-product-bets-and-alpha-recommendation-2026-09-09.md).

### Repository boundaries

- This repository is Belay Local. Do not add or modify `belay-cloud`.
- Product support is Claude Code and Codex only. Do not claim Cursor support
  until a native Cursor transcript and cost reader exists.
- Belay itself makes no product-network calls. New HTTP surfaces must remain
  authenticated and loopback-only.
- Preserve the encrypted SQLite store, ordered migrations, canonical event
  model, Numbat integration, and governed MCP tool catalog. Extend alongside
  these components rather than replacing them.
- Transcript text is local-only and secret-scrubbed before encrypted storage.
- Agent activity and transcript excerpts are untrusted evidence, never
  instructions or authorization.
- Post-fix recurrence and before-versus-after cost verification are currently
  parked; do not claim that a proposed or applied fix worked.

### Architecture map

- `cmd/belay`: CLI commands, quickstart, Local runtime, and MCP configuration.
- `internal/acquisition/numbat`: pinned Numbat acquisition integration.
- `internal/acquisition/transcript`: native Claude Code and Codex JSONL readers.
- `internal/canonical`: canonical event model, Numbat mapping, and source
  catalog.
- `internal/detection` and `internal/issueintel`: deterministic issue detection
  and cost-ranked issue models.
- `internal/localapp`: application orchestration, transcript analysis, semantic
  analysis, Mission Packs, skills, and fix workflows.
- `internal/storage/local`: encrypted SQLite persistence, migrations, and
  projections.
- `internal/presentation/readmodel`: bounded product read models.
- `internal/presentation/localhttp`: authenticated loopback API and embedded
  vanilla JavaScript browser.
- `internal/presentation/localmcp`: local stdio MCP server and tool contracts.
- `scripts`, `Makefile`, and `docs/launch`: packaging and alpha release gates.

The main value path is:

```text
Claude Code / Codex JSONL ─┐
                           ├─ encrypted Local store ─ detectors ─ Report
Numbat canonical events ───┘                                  ├─ Mission Packs
                                                              └─ MCP evidence
```

### First checkout

Go 1.27 is required. Node.js is used for the embedded browser syntax check.

```bash
git status --short
git branch --show-current
go version
make verify
node --check internal/presentation/localhttp/assets/app.js
git diff --check
```

### Isolated cross-harness value proof

`cmd/belay-eval` is an engineering-only runner, not an end-user Belay command.
It uses a disposable encrypted store and never reads the developer's Belay
store. The full C5 proof invokes Claude with no session persistence and Codex
with `--ephemeral`, then records exact local artifacts:

```bash
GOCACHE=/tmp/belay-engine-go-cache go run ./cmd/belay-eval \
  --real-claude \
  --real-codex \
  --root /tmp/belay-c5-proof \
  --output /tmp/belay-c5-proof/result.json
```

The proof must show a bound Codex receipt, `verifier_state: satisfied`, cited
command/result turns, and `post_pause_experience_count: 0`. The original
Claude proposal and Codex JSON events remain beside the result for audit.

The C6 engineering pilot runs all five required delivery baselines with a fixed
model and deterministic exact-command scorer. It preserves failed and neutral
results:

```bash
GOCACHE=/tmp/belay-engine-go-cache go run ./cmd/belay-eval \
  --comparative \
  --repetitions 3 \
  --model openai.gpt-5.6-sol \
  --root /tmp/belay-c6-eval \
  --output /tmp/belay-c6-eval/result.json
```

This pilot combines the production-path C5 verifier proof with paired
behavioral runs. Its compiled baseline uses approved Mission Pack text plus the
experiment's exact command/exit-code scorer; it does not claim that the
comparative runner itself is another product runtime.

The C9 exporter binds a semantic proposal to its cited evidence, outcomes,
required baselines, metrics, and explicit readiness gaps. It is
non-authoritative and must read only a disposable SQLite backup:

```bash
GOCACHE=/tmp/belay-engine-go-cache go run ./cmd/belay-eval \
  --private-eval-capsule \
  --capsule-db /tmp/belay-eval-copy/belay.sqlite \
  --capsule-proposal exs_example \
  --output /tmp/belay-eval-copy/capsule.json
```

Never point this mode at a live Belay database. A draft capsule is not
executable until the source repository revision, task setup, and deterministic
task outcome have been reconstructed. Exporting it grants no instruction
authority.

For focused iteration, run the package closest to the change:

```bash
go test -count=1 ./cmd/belay
go test -count=1 ./internal/localapp
go test -count=1 ./internal/missionpack
go test -count=1 ./internal/presentation/localhttp
go test -count=1 ./internal/presentation/localmcp
go test -count=1 ./internal/storage/local
go test -count=1 ./internal/detection/...
```

Run `make verify` again before pushing. If timing-sensitive local-store tests
contend under package parallelism, confirm the failures in isolation and use
`GOFLAGS=-p=1 make verify` for a stable release checkpoint; do not hide a
reproducible failure.

### Working on the product

Read the implementation before changing behavior:

1. Trace the CLI entry point in `cmd/belay`.
2. Trace orchestration in `internal/localapp`.
3. Inspect the relevant SQLite projection and migration.
4. Inspect both browser and MCP consumers of the read model.
5. Preserve evidence traceability from every displayed number or claim back to
   stored turns or canonical events.

Current priority order:

1. Make Report and Mission Packs consistently useful on real multi-session
   datasets.
2. Keep first-run output fast, concise, and understandable without internal
   pipeline terminology.
3. Complete clean-machine and corporate-device alpha validation.
4. Evaluate Eval Forge only after users repeatedly trust and use Mission Packs.
5. Consider Swarm Governor only after multi-agent coordination pain is
   demonstrated.

Before running state-changing onboarding or release commands, read
[`docs/launch/developer-preview.md`](docs/launch/developer-preview.md) and
[`docs/launch/clean-machine-alpha-qa.md`](docs/launch/clean-machine-alpha-qa.md).
`quickstart` may install hooks, MCP configuration, skills, and Keychain-backed
Local state.

## Build the alpha locally

Go 1.27 is required to build from source. From a clean checkout on Apple
Silicon:

```bash
make verify
make preview ALPHA_VERSION=0.0.1-alpha.5 ALPHA_ARCH=arm64
```

Run the complete non-publishing automated readiness path:

```bash
make alpha-readiness ALPHA_VERSION=0.0.1-alpha.5
```

That target verifies source and release-surface checks, builds the arm64
archive, verifies its checksum, runs the packaged smoke test, and prints the
remaining manual gates. It never signs, notarizes, tags, publishes, releases,
deploys, or modifies real Belay/harness state.

The expected files are:

```text
dist/belay-local-developer-alpha-v0.0.1-alpha.5-darwin-arm64.tar.gz
dist/belay-local-developer-alpha-v0.0.1-alpha.5-darwin-arm64.tar.gz.sha256
```

No artifact is published merely by running these commands.

## Verify and run

Obtain the archive and checksum through an explicitly authorized channel. From
the directory containing both files:

```bash
shasum -a 256 -c \
  belay-local-developer-alpha-v0.0.1-alpha.5-darwin-arm64.tar.gz.sha256

tar -xzf belay-local-developer-alpha-v0.0.1-alpha.5-darwin-arm64.tar.gz
cd belay-local-developer-alpha-v0.0.1-alpha.5-darwin-arm64

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

## Local MCP

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
- `get_top_issues`
- `get_issue_excerpts`
- `get_fix_status`
- `get_mission_pack`
- `record_mission_pack_accepted`
- `get_mission_pack_status`
- `list_experience_proposals`
- `list_active_experiences`
- `approve_experience`
- `resolve_experience_proposal`
- `prepare_experience_lifecycle`
- `apply_experience_lifecycle`
- `propose_fix`
- `record_fix_applied`

The MCP implementation is `1.7.0`. `get_mission_pack` uses
`mission-pack.det.v3` to prepare bounded, inactive guidance for the current
project. Managed Belay skill calls—`/belay` in Claude Code and `$belay` in
Codex—pass the actual host harness and a concise active-task hint when one
exists. Without a current harness, Belay omits semantic rules; without a
relevant task hint, it does not
surface unrelated unanchored correction clusters. Stale, low-confidence, or
unsupported semantic rules are omitted, cross-harness targets are safely
adapted or suppressed, and at most three verification commands are selected
for the stated intent. A pack with no actionable traps, rules, or verification
commands is `empty` and cannot be activated. Mission Pack preparation does not
verify later recurrence or cost reduction.

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
Recurrence monitoring remains available through authenticated Local
HTTP/browser routes only. MCP mutations are bounded to explicit user-governed
records and lifecycle decisions. `propose_fix` stores a bounded unified-diff
proposal for an allowlisted harness configuration file but never applies it;
other governed tools can record an approved application, Mission Pack
acceptance, or experience-learning decision. No MCP tool executes a command or
writes a project file.

Exact Codex and Claude Code configuration examples are in
[`docs/launch/developer-preview.md`](docs/launch/developer-preview.md).

## Privacy and evidence limitations

- Belay Local sends no product telemetry and requires no Doplex service.
- Belay Local and its MCP server make no product-network request. A configured
  MCP client or remotely hosted model may process or transmit tool results
  according to that product's privacy policy and the user's configuration.
- Prompt bodies, transcripts, file contents, raw endpoint identity, and raw
  evidence paths are excluded from canonical events.
- Belay Local separately retains secret-scrubbed transcript turns in encrypted
  on-device storage for loopback-only local analysis. Belay does not upload
  this content.
- Minimized envelope/index fields remain plaintext in SQLite; canonical event
  JSON, finding citations, and transcript payloads are encrypted.
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
- includes both binaries, Belay's MIT license, and Numbat's exact Apache-2.0 license
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
- Product direction:
  [`docs/product/belay-product-bets-and-alpha-recommendation-2026-09-09.md`](docs/product/belay-product-bets-and-alpha-recommendation-2026-09-09.md)

The normal CI workflow validates code and release-surface checks. The separate
Developer Alpha workflow is manual-only. It always uploads a short-lived
workflow artifact and publishes an unsigned GitHub prerelease only when its
explicit `publish` input is true.
