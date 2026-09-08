# Contributing to Belay Local

Belay Local is an Apache-2.0 open-source edge project. Contributions should
preserve its endpoint-first, privacy-minimized, fail-open design.

## Before starting

1. Read `README.md`, `docs/launch/local-v0-requirements.md`, and the applicable
   contract documents.
2. Search existing issues and discussions when the repository is public.
3. For a substantial or contract-changing proposal, open a design discussion
   before implementation.

## Non-negotiable boundaries

- Do not modify or patch Numbat source. Belay consumes a pristine pinned
  upstream revision through its executable interface.
- Do not add write-capable MCP, enforcement, remediation, or agent-action
  dependencies.
- Do not add Local product telemetry or hosted model calls.
- Do not persist prompt bodies, transcripts, reasoning text, file contents, raw
  endpoint identity, or raw evidence paths in canonical Belay events.
- Do not include real customer data, credentials, personal paths, or transcripts
  in tests, issues, or pull requests.
- Agent actions must remain usable when Belay or Numbat fails.

## Development checks

Go 1.27 is required.

```bash
make verify
```

For release-surface-only changes:

```bash
make verify-release-surface
```

On an Apple Silicon Mac, a local non-publishing package check is:

```bash
make alpha-readiness ALPHA_VERSION=0.0.1-alpha.1
```

The packaging command requires a clean checkout by default. A dirty artifact
created with `BELAY_ALPHA_ALLOW_DIRTY=1` is for local validation only and must
not be distributed.

## Pull requests

Keep changes focused and explain:

- the user-visible behavior;
- privacy and fail-open impact;
- tests and commands run;
- documentation or contract changes;
- known limitations;
- whether the change affects package contents or checksums.

Do not commit generated `/dist/` or `/bin/` output. Do not create tags, releases,
or deployments from a contribution unless maintainers explicitly authorize that
separate action.

## Security issues

Follow `SECURITY.md`. Do not disclose suspected vulnerabilities or sensitive
evidence in a public issue.
