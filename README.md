# Belay Engine

Belay Local is private, endpoint-first observability for developers using
multiple AI agent harnesses. It reconstructs minimized local activity into one
timeline and exposes the same evidence through a loopback browser and read-only
MCP.

This repository contains the public Belay edge. Belay Teams is separate and is
not part of the individual-developer preview.

## Belay Local

- One machine, accountless, and usable offline
- No product telemetry or Belay model calls
- Encrypted local SQLite storage backed by macOS Keychain
- Historical and explicitly enabled live collection through pristine Numbat
- Loopback-only browser with a per-launch token
- Six read-only MCP tools
- Launch-validated with Codex and Claude Code

Belay never sits in the agent action path. It does not provide write-capable
MCP, enforcement, remediation, fix recording, or recurrence registration.

## Unsigned developer preview

The current community launch is an **unsigned, unnotarized developer preview**
for individual macOS users. It is not a production release or installer.

Supported targets:

- `darwin/arm64` — Apple Silicon
- `darwin/amd64` — Intel Mac

Go 1.27 is required to build from source. The preview packages Belay with the
exact approved, unmodified Numbat research commit:

```text
f0778c09dc48281aa93a3887d05096c0a1f3f9f7
```

### Build and package

From a clean checkout:

```bash
make verify
make preview PREVIEW_VERSION=0.0.1-dev.1 PREVIEW_ARCH=native
```

Build both macOS architectures:

```bash
make preview-all PREVIEW_VERSION=0.0.1-dev.1
```

The build remains local. It does not sign, notarize, publish, tag, deploy, or
create a GitHub Release. Generated `/dist/` and `/bin/` directories are ignored
so a normal preview build does not dirty a clean checkout.

### Verify the archive

```bash
(
  cd dist
  shasum -a 256 -c \
    belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64.tar.gz.sha256
)

scripts/smoke-developer-preview.sh \
  dist/belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64.tar.gz
```

Use the archive matching your Mac's architecture. See
[`docs/launch/developer-preview.md`](docs/launch/developer-preview.md) for
Gatekeeper instructions, MCP configuration, offline behavior, uninstall steps,
known limitations, and the exact archive contract.

## Run Belay Local

After verifying and extracting the archive:

```bash
cd belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64
NUMBAT_SHA256="$(shasum -a 256 ./bin/numbat | awk '{print $1}')"

./bin/belay local \
  --numbat ./bin/numbat \
  --numbat-sha256 "${NUMBAT_SHA256}" \
  --numbat-version-marker f0778c09dc48
```

The command initializes private state under `${BELAY_HOME:-~/.belay}`, scans
supported history, and prints a loopback browser URL. Hook installation remains
explicit:

```bash
./bin/belay hooks install
./bin/belay hooks status
```

Run the read-only MCP server over stdio:

```bash
./bin/belay mcp
```

The six tools are `list_sessions`, `get_session`, `get_session_timeline`,
`query_activity`, `list_findings`, and `get_stats`.

## Build integrity and licenses

The packaging script:

- refuses a dirty or wrong Numbat checkout;
- verifies exact upstream license hashes;
- builds Belay and Numbat with `CGO_ENABLED=0`, `-trimpath`, and no VCS build
  stamping;
- includes both binaries, Belay's Apache-2.0 license, and Numbat's exact license
  and third-party attribution;
- creates internal `SHA256SUMS` plus an archive checksum;
- emits deterministic archive metadata from `SOURCE_DATE_EPOCH`.

Belay is licensed under Apache-2.0. Numbat attribution is vendored under
[`licenses/numbat`](licenses/numbat).

## Development

```bash
make verify
```

The normal CI workflow tests and builds `main`. The separate developer-preview
workflow is manual-only and uploads short-lived workflow artifacts; it contains
no release or publishing step.

Architecture and contract references:

- [`docs/launch/local-v0-requirements.md`](docs/launch/local-v0-requirements.md)
- [`docs/contracts/event-envelope-v1.md`](docs/contracts/event-envelope-v1.md)
- [`docs/contracts/read-api-v1.md`](docs/contracts/read-api-v1.md)
- [`docs/contracts/mcp-v1.md`](docs/contracts/mcp-v1.md)
- [`docs/storage/local-storage-lifecycle.md`](docs/storage/local-storage-lifecycle.md)
