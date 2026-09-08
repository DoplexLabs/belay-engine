# Belay Local unsigned developer preview

This preview is for individual macOS developers testing local, accountless
observability across AI coding agents. It is not a production release, is not
signed or notarized, and does not include Belay Teams.

## Supported systems

- macOS on Apple Silicon (`darwin/arm64`)
- macOS on Intel (`darwin/amd64`)
- Go 1.27 is required only when building the archive from source
- Launch validation covers Codex and Claude Code

Linux and Windows packages are not part of this preview. The binaries are built
with `CGO_ENABLED=0`, but Local storage currently depends on macOS Keychain.

## Build an unsigned archive

Use a clean Belay checkout. The packaging script fetches the exact approved,
unmodified Numbat commit
`f0778c09dc48281aa93a3887d05096c0a1f3f9f7`, verifies that its checkout and
license files are pristine, builds both binaries, and creates local archives.

```bash
make verify
make preview PREVIEW_VERSION=0.0.1-dev.1 PREVIEW_ARCH=native
```

Build both supported architectures:

```bash
make preview-all PREVIEW_VERSION=0.0.1-dev.1
```

Generated `/dist/` and `/bin/` directories are ignored by Git, so a normal
preview build does not dirty a clean checkout.

Use a pre-existing Numbat checkout for an offline or pre-fetched build:

```bash
scripts/build-developer-preview.sh \
  --version 0.0.1-dev.1 \
  --arch arm64 \
  --numbat-source /absolute/path/to/pristine/numbat \
  --output-dir ./dist
```

The supplied checkout must be clean and exactly at the approved commit. The
script fails closed for any other commit, local modification, license mismatch,
or incomplete pin. The build uses a command-scoped redirect for the historical
`github.com/google/cel-go` module location; it does not edit Numbat or global Git
configuration.

Each archive contains:

- `bin/belay`
- `bin/numbat`
- Belay's Apache-2.0 `LICENSE`
- Numbat's exact `LICENSE` and `THIRD_PARTY_LICENSES.txt`
- `BUILD-INFO.txt`
- `SHA256SUMS`
- this guide and the repository README

The companion `.tar.gz.sha256` file covers the archive itself. Given the same
Belay tree, Numbat commit, Go toolchain, target architecture, version, and
`SOURCE_DATE_EPOCH`, archive metadata and entry ordering are deterministic.

## Verify and smoke-test

Verify the downloaded or locally built archive before extraction:

```bash
cd /path/to/downloads
shasum -a 256 -c \
  belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64.tar.gz.sha256
```

Run the packaged smoke test from a Belay source checkout:

```bash
scripts/smoke-developer-preview.sh \
  ./dist/belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64.tar.gz
```

The smoke test checks archive paths and contents, verifies every internal
checksum, confirms the pinned Numbat version, runs Belay's verification command,
and exercises help plus read-only agent inventory using fresh temporary user and
Belay homes. It cannot discover the real user's agent configuration, never
installs hooks, and never opens the Local database. When `sandbox-exec` is
operational, runtime checks execute with network access denied.

## Unsigned macOS warning

These preview binaries are intentionally unsigned and not notarized. macOS may
block the first launch because the archive came from the internet.

After checking the SHA-256 checksum:

1. Try to open or run `belay` once.
2. Open **System Settings → Privacy & Security**.
3. In the security section, choose **Open Anyway** for the specific Belay
   binary, then confirm.

You can also Control-click the specific binary in Finder and choose **Open**.
Do not disable Gatekeeper or other system-wide protections.

## First run

Extract the archive and keep `belay` and `numbat` together:

```bash
tar -xzf belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64.tar.gz
cd belay-local-developer-preview-v0.0.1-dev.1-darwin-arm64
NUMBAT_SHA256="$(shasum -a 256 ./bin/numbat | awk '{print $1}')"
./bin/belay local \
  --numbat ./bin/numbat \
  --numbat-sha256 "${NUMBAT_SHA256}" \
  --numbat-version-marker f0778c09dc48
```

Hook installation is always explicit:

```bash
./bin/belay hooks install
./bin/belay hooks status
```

Belay does not install hooks during ordinary `local`, `scan`, `agents`, `mcp`,
or `doctor` commands.

### Reading event outcomes

The pinned artifact feed may not report an outcome for every event. In the
Local browser, only explicitly reported `Succeeded`, `Failed`, or `Interrupted`
event outcomes receive prominent status badges. Source-unreported event
outcomes remain honest and visible as subdued
`Outcome · Not reported by source` metadata. Session-level outcome badges,
including `Incomplete` and `Unknown`, are unchanged.

## Keychain troubleshooting

Belay creates its Local encryption key through macOS Keychain without asking
for password data. If `/usr/bin/security` prints `password data for new item:`,
stop Belay instead of entering your login password. That prompt identifies an
affected pre-hotfix build; update to a build containing the noninteractive
Keychain creation fix before retrying.

## Read-only MCP

Belay MCP uses stdio and exposes exactly six read-only tools. Use an absolute
path to the extracted `belay` binary.

### Codex

Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.belay]
command = "/absolute/path/to/belay-preview/bin/belay"
args = ["mcp"]
```

### Claude Code

Add a project-scoped `.mcp.json`:

```json
{
  "mcpServers": {
    "belay": {
      "type": "stdio",
      "command": "/absolute/path/to/belay-preview/bin/belay",
      "args": ["mcp"]
    }
  }
}
```

Restart the client after changing its MCP configuration. Belay MCP reads the
same encrypted Local store as the browser and cannot install hooks, execute
commands, modify files, or perform remediation.

## Offline behavior

Building from source normally requires network access to fetch Git and Go
dependencies. Once the archive exists, Belay Local's browser, encrypted store,
historical scan, live import, and MCP do not require a hosted Belay service or
internet connection. No product telemetry is sent.

The first run uses only the packaged Numbat executable and local agent
artifacts. The loopback browser binds to `127.0.0.1` and requires a per-launch
token.

The manually triggered GitHub developer-preview workflow passes its requested
version into shell steps through a quoted environment variable. The packaging
script validates the version before using it in a path. The workflow uploads
only short-lived workflow artifacts and has no release or publishing step.

## Uninstall and cleanup

If you explicitly installed hooks, remove them before deleting the package:

```bash
./bin/belay hooks uninstall
```

Then stop Belay and delete the extracted preview directory.

Belay data remains under `${BELAY_HOME:-~/.belay}`. Back up anything you want to
keep, then delete that directory only if you want to remove the encrypted Local
database, configuration, cursors, and cached Numbat binary. Keychain entries use
the service name `dev.doplex.belay.local.data-key.v1`; remove them through
Keychain Access only after deleting the corresponding database.

Remove the Belay entries from Codex or Claude Code MCP configuration separately.

## Known limitations

- Only Codex and Claude Code have sanitized Belay end-to-end launch fixtures.
- Additional agents reported by Numbat are upstream-supported, not
  launch-validated by Belay.
- This preview is source/workflow-artifact distribution, not a signed installer.
- There is no automatic updater.
- The pinned Numbat dependency is an approved research commit, not a stable
  upstream release tag.
- The browser and MCP are local read surfaces; Teams is not included.
- The smoke test avoids the macOS Keychain and therefore does not replace a
  manual encrypted-store acceptance test.

## Production blockers

- Apple Developer ID signing and notarization
- A production Numbat release pin with the required schema
- Signed installer/update-channel design
- Broader harness compatibility and clean-machine acceptance coverage
- Formal versioned release/tag policy and supported upgrade guarantees
