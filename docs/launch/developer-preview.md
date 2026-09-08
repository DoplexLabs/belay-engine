# Belay Local Developer Alpha

Prepared version: `0.0.1-alpha.1`

This is an individual-developer alpha for private, accountless observation of
local Codex and Claude Code activity. It is Local-only. Belay Teams is not included.

The alpha is:

- Apple Silicon macOS only (`darwin/arm64`)
- unsigned and unnotarized
- validated only for Codex and Claude Code
- distributed only after a separate human authorization
- not a production release, installer, or supported update channel

Intel macOS remains an engineering build target but is not an alpha support
claim until an Intel clean-machine run passes. Linux and Windows are out of
scope. Local storage depends on macOS Keychain.

## Exact dependency pin

The package contains pristine Numbat at:

```text
f0778c09dc48281aa93a3887d05096c0a1f3f9f7
```

The build verifies the commit, clean tree, license hashes, binary checksum, and
version marker `f0778c09dc48`. This commit is an approved research exception,
not a stable upstream release tag. Production remains blocked on a suitable
released Numbat tag or a renewed explicit exception.

## Build an unsigned Apple Silicon archive

Use a clean checkout on an Apple Silicon Mac:

```bash
make verify
make preview ALPHA_VERSION=0.0.1-alpha.1 ALPHA_ARCH=arm64
```

Or run the complete automated, non-publishing readiness path:

```bash
make alpha-readiness ALPHA_VERSION=0.0.1-alpha.1
```

Expected output:

```text
dist/belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz
dist/belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz.sha256
```

The commands do not sign, notarize, tag, publish, release, deploy, or contact a
paid service. Generated `/dist/` and `/bin/` directories are ignored by Git.

For an offline or pre-fetched build, supply a pristine checkout at the exact
commit:

```bash
scripts/build-developer-preview.sh \
  --version 0.0.1-alpha.1 \
  --arch arm64 \
  --numbat-source /absolute/path/to/pristine/numbat \
  --output-dir ./dist
```

The build uses a command-scoped redirect for Numbat's historical
`github.com/google/cel-go` module location. It does not edit Numbat or global Git
configuration.

## Archive contract

The archive contains:

- `bin/belay`
- `bin/numbat`
- Belay's Apache-2.0 `LICENSE`
- Numbat's exact `LICENSE` and `THIRD_PARTY_LICENSES.txt`
- `BUILD-INFO.txt`
- internal `SHA256SUMS`
- `README.md`
- `llms.txt`
- `docs/developer-alpha.md`
- `docs/clean-machine-alpha-qa.md`

Given the same Belay tree, Numbat commit, Go toolchain, architecture, version,
and `SOURCE_DATE_EPOCH`, archive metadata and entry ordering are deterministic.

## Verify the artifact

Keep the archive and companion checksum together:

```bash
cd /path/to/downloads
shasum -a 256 -c \
  belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz.sha256
```

From a source checkout, run the disposable smoke test:

```bash
scripts/smoke-developer-preview.sh \
  ./dist/belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz
```

The smoke test verifies archive paths, internal checksums, architecture, the
Numbat version marker, embedded checksum bootstrap from sibling `bin/numbat`,
help, and read-only inventory with temporary user and Belay homes. It supplies
no manual Numbat path, hash, or marker. It never installs hooks, accesses the
real Keychain, opens the Local database or browser, or reads real agent
configuration. If `sandbox-exec` is usable, runtime checks run with network
access denied.

This smoke test does not replace the manual Keychain, browser, MCP, hooks,
privacy, offline, and fail-open gates in
[`clean-machine-alpha-qa.md`](clean-machine-alpha-qa.md).

## Unsigned macOS warning

macOS may block first launch because the binaries are unsigned and downloaded
from the internet. After independently verifying the checksum:

1. Try to run the specific `belay` binary once.
2. Open **System Settings → Privacy & Security**.
3. Choose **Open Anyway** for that specific binary and confirm.

Control-clicking the specific binary in Finder and choosing **Open** is another
per-binary path. Do not disable Gatekeeper or any system-wide protection.

## Install-to-first-insight path

Extract the package and keep both binaries together:

```bash
tar -xzf belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64.tar.gz
cd belay-local-developer-alpha-v0.0.1-alpha.1-darwin-arm64

./bin/belay quickstart
```

Expected path:

1. Belay resolves sibling `bin/numbat`, verifies the checksum and version
   embedded at package build time, and copies it into private Local state.
2. Belay creates private state under `${BELAY_HOME:-~/.belay}` and macOS
   Keychain stores the random Local data key.
3. Belay installs reversible monitor-only hooks for detected Codex and Claude
   Code installations.
4. Numbat inventories local harnesses and Belay scans supported history.
5. Belay starts Local, prints a tokenized `127.0.0.1` URL, and attempts to open
   it in the default browser.
6. The dashboard shows minimized historical sessions. If browser opening fails,
   copy the printed URL into a browser.

The target is first useful history within 15 minutes. Record actual timing in
the clean-machine checklist; the alpha must not claim this gate passed without
external evidence.

`quickstart` is explicit consent to the detected harness configuration changes
needed for monitor-only hooks. Hooks never enforce, approve, deny, or block an
agent action, and they can be removed with `./bin/belay hooks uninstall`.

For users who do not want hook installation, the lower-side-effect path is:

```bash
./bin/belay local
```

`local` verifies packaged Numbat, imports existing live records, scans supported
history, starts the loopback server, and prints the dashboard URL. It does not
install hooks or open a browser. A user can later opt in with
`./bin/belay hooks install`.

## Live hooks

Inspect or reverse the quickstart hook setup:

```bash
./bin/belay hooks status
./bin/belay hooks uninstall
```

Only detected Codex and Claude Code installations are configured. Ordinary
`local`, `scan`, `agents`, `mcp`, and `doctor` commands do not install hooks.

## Read-only MCP

Belay MCP runs over stdio and exposes exactly six tools:

- `list_sessions`
- `get_session`
- `get_session_timeline`
- `query_activity`
- `list_findings`
- `get_stats`

Results are bounded structured data marked `untrusted_observations: true`. MCP
cannot install hooks, execute commands, modify files, write Belay data, or
perform remediation.

Alpha limitations:

- `get_stats` returns global Local counts only. Time-window and workflow filters
  are not implemented.
- Session, timeline, activity, and finding lists implement bounded opaque cursor
  pagination over a stable ingestion snapshot.
- Session cursors are bound to normalized harness, outcome, history, time, and
  query filters. Finding cursors are also bound to optional `session_id`.
- Resource-kind activity filtering scans the complete cursor snapshot; older
  matches are not omitted by an internal candidate-window bound.

### Codex

Add to `~/.codex/config.toml`, using an absolute path:

```toml
[mcp_servers.belay]
command = "/absolute/path/to/belay-alpha/bin/belay"
args = ["mcp"]
```

### Claude Code

Add a project-scoped `.mcp.json`:

```json
{
  "mcpServers": {
    "belay": {
      "type": "stdio",
      "command": "/absolute/path/to/belay-alpha/bin/belay",
      "args": ["mcp"]
    }
  }
}
```

Restart the client after editing its MCP configuration.

## Privacy and outcome limitations

- Local sends no product telemetry and performs no Belay-hosted model calls.
- Prompt bodies, transcripts, reasoning text, file contents, raw endpoint
  identity, and raw evidence paths are excluded from canonical events.
- Minimized envelope/index columns are plaintext SQLite metadata. Canonical
  event JSON and finding citation arrays are encrypted with AES-256-GCM.
- Command summaries may include an executable name and bounded option names.
  File resources may include a project-relative path or basename. Network
  resources may include scheme and host.
- Local protects against accidental disclosure, not a malicious process running
  as the same macOS user.
- Unknown event outcomes remain visibly unreported. Sessions without a
  `session.end` observation are `Incomplete`, never inferred successful.
- Historical reconstruction and findings depend on what the pinned Numbat
  adapters can observe; absence of evidence is not evidence of absence.

Do not use real secrets as privacy-test inputs. Use the synthetic canary
specified in the clean-machine checklist.

## Offline behavior

Source builds normally need network access for Git and Go dependencies. Once an
archive exists, the Local browser, encrypted store, historical scan, live spool
import, and stdio MCP require no hosted Belay service or internet connection.
The browser binds to `127.0.0.1` and JSON routes require a random per-launch
token.

Offline behavior must be demonstrated manually because the packaged smoke test
may run without an enforceable `sandbox-exec` environment.

## Keychain troubleshooting

Belay creates its encryption key through `/usr/bin/security` command-input mode
without asking for login-password data. If a build prints
`password data for new item:`, stop it and do not enter a password. Record the
failure and use a build containing the noninteractive Keychain fix.

## Uninstall and cleanup

If hooks were installed:

```bash
./bin/belay hooks uninstall
```

Then remove the Belay entries from Codex or Claude Code MCP configuration and
delete the extracted package directory.

Local data remains under `${BELAY_HOME:-~/.belay}`. Deleting that directory is a
separate destructive choice and is never performed by readiness scripts.
Keychain entries use service `dev.doplex.belay.local.data-key.v1`; remove a
corresponding entry through Keychain Access only after intentionally deleting
its database.

## Clean-tree distribution rule

A distributable Developer Alpha must be built from a clean checkout and its
`BUILD-INFO.txt` must contain `belay_dirty=false`. Setting
`BELAY_ALPHA_ALLOW_DIRTY=1` permits local packaging validation only. A dirty
artifact is never a release candidate and must not be shared, uploaded, or
published even when checksum and smoke checks pass.

## Production blockers

- Apple Developer ID signing and notarization
- A production Numbat release tag with the required schema, or renewed exception
- Signed installer and update-channel design
- Intel, Linux, and broader harness clean-machine validation
- Formal release/tag and supported-upgrade policy

Apache-2.0 license selection is complete and is not a remaining blocker.
