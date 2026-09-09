#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

readonly ALPHA_VERSION="0.0.1-alpha.1"
readonly NUMBAT_COMMIT="f0778c09dc48281aa93a3887d05096c0a1f3f9f7"

die() {
  printf 'validate-alpha-surface: %s\n' "$*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || die "required release-surface file is missing: $1"
}

require_text() {
  local path="$1"
  local text="$2"
  grep -Fq -- "${text}" "${path}" ||
    die "${path} does not contain required text: ${text}"
}

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repository_root="$(cd -- "${script_dir}/.." && pwd -P)"

command -v grep >/dev/null 2>&1 || die "required command not found: grep"

for path in \
  README.md \
  SECURITY.md \
  CONTRIBUTING.md \
  SUPPORT.md \
  llms.txt \
  docs/contracts/mcp-v1.md \
  docs/design/p0-09-one-command-onboarding.md \
  docs/implementation/briefs/14-p0-one-command-onboarding.md \
  docs/launch/developer-preview.md \
  docs/launch/clean-machine-alpha-qa.md \
  docs/launch/local-v0-requirements.md; do
  require_file "${repository_root}/${path}"
done

for path in \
  README.md \
  docs/launch/developer-preview.md \
  Makefile \
  scripts/build-developer-preview.sh \
  .github/workflows/developer-preview.yml; do
  require_text "${repository_root}/${path}" "${ALPHA_VERSION}"
done

for path in \
  README.md \
  docs/launch/developer-preview.md \
  llms.txt \
  scripts/build-developer-preview.sh; do
  require_text "${repository_root}/${path}" "${NUMBAT_COMMIT}"
done

require_text "${repository_root}/README.md" "Apple Silicon"
require_text "${repository_root}/docs/launch/developer-preview.md" "Apple Silicon"
require_text "${repository_root}/docs/launch/developer-preview.md" "unsigned"
require_text "${repository_root}/docs/launch/developer-preview.md" "Codex"
require_text "${repository_root}/docs/launch/developer-preview.md" "Claude Code"
require_text "${repository_root}/docs/launch/developer-preview.md" "Belay Teams is not included"
require_text "${repository_root}/docs/launch/local-v0-requirements.md" "Apache-2.0"
require_text "${repository_root}/README.md" "stable ingestion snapshot"
require_text "${repository_root}/docs/launch/developer-preview.md" "stable ingestion snapshot"
require_text "${repository_root}/docs/launch/local-v0-requirements.md" "next_cursor"
require_text "${repository_root}/README.md" "./bin/belay quickstart"
require_text "${repository_root}/docs/launch/developer-preview.md" "./bin/belay quickstart"
require_text "${repository_root}/docs/launch/clean-machine-alpha-qa.md" "./bin/belay quickstart"
require_text "${repository_root}/README.md" "./bin/belay quickstart --no-mcp"
require_text "${repository_root}/README.md" \
  "./bin/belay quickstart --allow-codex-mcp-add"
require_text "${repository_root}/docs/launch/developer-preview.md" \
  "./bin/belay quickstart --no-mcp"
require_text "${repository_root}/docs/launch/developer-preview.md" \
  "./bin/belay quickstart --allow-codex-mcp-add"
require_text "${repository_root}/docs/launch/clean-machine-alpha-qa.md" \
  "./bin/belay quickstart --no-mcp"
require_text "${repository_root}/docs/launch/clean-machine-alpha-qa.md" \
  "./bin/belay quickstart --allow-codex-mcp-add"
require_text "${repository_root}/README.md" "./bin/belay local"
require_text "${repository_root}/docs/launch/developer-preview.md" "./bin/belay local"
require_text "${repository_root}/docs/launch/local-v0-requirements.md" "belay quickstart"
for path in \
  README.md \
  llms.txt \
  docs/launch/developer-preview.md \
  docs/launch/clean-machine-alpha-qa.md \
  docs/design/p0-09-one-command-onboarding.md \
  docs/implementation/briefs/14-p0-one-command-onboarding.md \
  docs/contracts/mcp-v1.md; do
  require_text "${repository_root}/${path}" "mcp-config install"
  require_text "${repository_root}/${path}" "mcp-config status"
  require_text "${repository_root}/${path}" "mcp-config uninstall"
  require_text "${repository_root}/${path}" "allow-codex-mcp-add"
done
require_text "${repository_root}/docs/launch/local-v0-requirements.md" \
  "allow-codex-mcp-add"
for path in \
  README.md \
  llms.txt \
  docs/launch/developer-preview.md \
  docs/launch/clean-machine-alpha-qa.md \
  docs/design/p0-09-one-command-onboarding.md \
  docs/implementation/briefs/14-p0-one-command-onboarding.md \
  docs/contracts/mcp-v1.md; do
  require_text "${repository_root}/${path}" "non-atomic"
  require_text "${repository_root}/${path}" "duplicate-name behavior"
done
for path in \
  README.md \
  llms.txt \
  docs/launch/developer-preview.md \
  docs/launch/clean-machine-alpha-qa.md \
  docs/contracts/mcp-v1.md; do
  for tool in \
    list_sessions \
    get_session \
    get_session_timeline \
    query_activity \
    list_findings \
    get_stats \
    list_issues \
    get_issue \
    lookup_session_events; do
    require_text "${repository_root}/${path}" "${tool}"
  done
done
require_text "${repository_root}/README.md" "foreign or unverifiable"
require_text "${repository_root}/docs/launch/developer-preview.md" \
  "foreign, scope-ambiguous"
require_text "${repository_root}/docs/launch/developer-preview.md" \
  "unverifiable entry"
require_text "${repository_root}/docs/contracts/mcp-v1.md" \
  "MCP uninstall does not remove monitor hooks"
require_text "${repository_root}/scripts/build-developer-preview.sh" \
  "main.bundledNumbatSHA256"
require_text "${repository_root}/scripts/build-developer-preview.sh" \
  "main.bundledNumbatVersionMarker"
require_text "${repository_root}/scripts/smoke-developer-preview.sh" \
  '"${belay}" agents'
require_text "${repository_root}/.github/workflows/developer-preview.yml" \
  'test "$(uname -m)" = "arm64"'

if grep -Eq \
  'NUMBAT_SHA256=|--numbat-sha256|--numbat-version-marker' \
  "${repository_root}/README.md" \
  "${repository_root}/docs/launch/developer-preview.md" \
  "${repository_root}/docs/launch/clean-machine-alpha-qa.md"; then
  die "release onboarding still requires a manual Numbat hash or version marker"
fi

if grep -Eq \
  -- '--numbat([[:space:]]|=)' \
  "${repository_root}/scripts/smoke-developer-preview.sh"; then
  die "packaged smoke must resolve sibling Numbat without a manual path"
fi

if grep -Eiq \
  'cursor pagination is (not implemented|unavailable)|cursor paging remains unavailable|resource-filtered activity may be a bounded subset|sparse resource filters can return a bounded subset' \
  "${repository_root}/README.md" \
  "${repository_root}/SUPPORT.md" \
  "${repository_root}/llms.txt" \
  "${repository_root}/docs/launch/developer-preview.md" \
  "${repository_root}/docs/launch/local-v0-requirements.md"; then
  die "release documentation contains a stale pagination or resource-filter limitation"
fi

if grep -Eiq \
  'exposes exactly six tools|MCP remains exactly six( read-only)? tools|server advertises exactly six tools' \
  "${repository_root}/README.md" \
  "${repository_root}/SUPPORT.md" \
  "${repository_root}/llms.txt" \
  "${repository_root}/docs/contracts/mcp-v1.md" \
  "${repository_root}/docs/launch/developer-preview.md" \
  "${repository_root}/docs/launch/clean-machine-alpha-qa.md" \
  "${repository_root}/docs/launch/local-v0-requirements.md"; then
  die "release documentation contains stale six-tool MCP wording"
fi

if grep -Eiq \
  'quickstart registers (the )?(stdio server|Belay MCP) automatically for (each )?detected supported agent|registers the existing read-only MCP server in detected Codex and Claude Code user configuration|quickstart success registers detected agents|attempts ownership-safe user-scope registration for detected Codex and Claude Code' \
  "${repository_root}/README.md" \
  "${repository_root}/llms.txt" \
  "${repository_root}/docs/contracts/mcp-v1.md" \
  "${repository_root}/docs/design/p0-09-one-command-onboarding.md" \
  "${repository_root}/docs/implementation/briefs/14-p0-one-command-onboarding.md" \
  "${repository_root}/docs/launch/developer-preview.md" \
  "${repository_root}/docs/launch/clean-machine-alpha-qa.md" \
  "${repository_root}/docs/launch/local-v0-requirements.md"; then
  die "release documentation overstates automatic Codex MCP registration"
fi

for path in \
  README.md \
  llms.txt \
  docs/contracts/mcp-v1.md \
  docs/design/p0-09-one-command-onboarding.md \
  docs/implementation/briefs/14-p0-one-command-onboarding.md \
  docs/launch/developer-preview.md \
  docs/launch/clean-machine-alpha-qa.md \
  docs/launch/local-v0-requirements.md; do
  require_text "${repository_root}/${path}" "mcp-config uninstall"
done

if grep -Eiq \
  'when updating a prior Codex registration|Updating Codex requires|Codex update additionally requires|then use the same update/rollback flow|archive move/recognized update with the Codex opt-in|verified identity is updated safely|Codex update requires.*allow-codex-mcp-add' \
  "${repository_root}/README.md" \
  "${repository_root}/llms.txt" \
  "${repository_root}/docs/contracts/mcp-v1.md" \
  "${repository_root}/docs/design/p0-09-one-command-onboarding.md" \
  "${repository_root}/docs/implementation/briefs/14-p0-one-command-onboarding.md" \
  "${repository_root}/docs/launch/developer-preview.md" \
  "${repository_root}/docs/launch/clean-machine-alpha-qa.md" \
  "${repository_root}/docs/launch/local-v0-requirements.md"; then
  die "release documentation incorrectly allows Codex registration migration"
fi

if grep -Fq 'if: runner.arch' \
  "${repository_root}/.github/workflows/developer-preview.yml"; then
  die "developer-alpha workflow may not conditionally skip native smoke"
fi

if grep -Eq \
  'softprops/action-gh-release|ncipollo/release-action|gh[[:space:]]+release|gh[[:space:]]+api.*releases|git[[:space:]]+push|git[[:space:]]+tag|npm[[:space:]]+publish|docker[[:space:]]+push|aws[[:space:]]+s3' \
  "${repository_root}/.github/workflows/developer-preview.yml"; then
  die "developer-alpha workflow contains a release or publishing command"
fi

printf 'alpha release-surface validation passed\n'
