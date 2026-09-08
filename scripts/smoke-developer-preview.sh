#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

readonly NUMBAT_VERSION_MARKER="f0778c09dc48"

die() {
  printf 'smoke-developer-preview: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

if (($# != 1)); then
  die "usage: scripts/smoke-developer-preview.sh PATH_TO_ARCHIVE.tar.gz"
fi

[[ "$(uname -s)" == "Darwin" ]] || die "runtime smoke tests require macOS"
for required in \
  awk basename chmod cut dirname env file find grep mkdir mktemp pwd rm shasum \
  sort tar uname; do
  require_command "${required}"
done

archive="$1"
[[ -f "${archive}" ]] || die "archive not found: ${archive}"
archive="$(cd -- "$(dirname -- "${archive}")" && pwd -P)/$(basename -- "${archive}")"

smoke_tmp="$(mktemp -d "${TMPDIR:-/tmp}/belay-preview-smoke.XXXXXX")"
cleanup() {
  rm -rf -- "${smoke_tmp}"
}
trap cleanup EXIT

listing="${smoke_tmp}/archive-contents.txt"
tar -tzf "${archive}" > "${listing}"
while IFS= read -r entry; do
  case "${entry}" in
    ""|/*|..|../*|*/../*)
      die "unsafe archive path: ${entry}"
      ;;
  esac
done < "${listing}"

top_levels="$(cut -d/ -f1 "${listing}" | LC_ALL=C sort -u)"
[[ "$(printf '%s\n' "${top_levels}" | grep -c .)" == "1" ]] ||
  die "archive must contain exactly one top-level directory"
package_name="${top_levels}"

required_entries=(
  "${package_name}/bin/belay"
  "${package_name}/bin/numbat"
  "${package_name}/LICENSE"
  "${package_name}/licenses/numbat/LICENSE"
  "${package_name}/licenses/numbat/THIRD_PARTY_LICENSES.txt"
  "${package_name}/README.md"
  "${package_name}/docs/developer-preview.md"
  "${package_name}/BUILD-INFO.txt"
  "${package_name}/SHA256SUMS"
)
for required_entry in "${required_entries[@]}"; do
  grep -Fxq "${required_entry}" "${listing}" ||
    die "archive is missing ${required_entry}"
done

tar -xzf "${archive}" -C "${smoke_tmp}"
package_root="${smoke_tmp}/${package_name}"
(
  cd -- "${package_root}"
  shasum -a 256 -c SHA256SUMS
)

belay="${package_root}/bin/belay"
numbat="${package_root}/bin/numbat"
[[ -x "${belay}" && -x "${numbat}" ]] || die "packaged binaries are not executable"

machine="$(uname -m)"
belay_file="$(file "${belay}")"
case "${machine}" in
  arm64)
    [[ "${belay_file}" == *"arm64"* ]] || die "archive is not native arm64"
    ;;
  x86_64)
    [[ "${belay_file}" == *"x86_64"* ]] || die "archive is not native amd64"
    ;;
  *)
    die "unsupported smoke-test architecture: ${machine}"
    ;;
esac

sandbox_profile='(version 1)(allow default)(deny network*)'
sandbox_available="false"
if command -v sandbox-exec >/dev/null 2>&1; then
  if sandbox-exec -p "${sandbox_profile}" /usr/bin/true >/dev/null 2>&1; then
    sandbox_available="true"
  else
    printf '%s\n' \
      "smoke-developer-preview: warning: sandbox-exec is present but unavailable; continuing without nested sandboxing" \
      >&2
  fi
fi

run_without_network() {
  if [[ "${sandbox_available}" == "true" ]]; then
    sandbox-exec -p "${sandbox_profile}" "$@"
  else
    "$@"
  fi
}

numbat_sha256="$(shasum -a 256 "${numbat}" | awk '{print $1}')"
run_without_network "${numbat}" version | grep -Fq "${NUMBAT_VERSION_MARKER}" ||
  die "packaged Numbat version marker mismatch"
run_without_network "${belay}" verify-numbat \
  --binary "${numbat}" \
  --sha256 "${numbat_sha256}" \
  --version-marker "${NUMBAT_VERSION_MARKER}"
run_without_network "${belay}" help >/dev/null

# This initializes only disposable user and Belay homes and performs read-only
# agent inventory. It never sees the real user home, invokes hook installation,
# or opens the Local database.
smoke_user_home="${smoke_tmp}/user-home"
smoke_home="${smoke_tmp}/belay-home"
mkdir -p -- "${smoke_user_home}"
chmod 0700 "${smoke_user_home}"
run_without_network env HOME="${smoke_user_home}" "${belay}" agents \
  --home "${smoke_home}" \
  --numbat "${numbat}" \
  --numbat-sha256 "${numbat_sha256}" \
  --numbat-version-marker "${NUMBAT_VERSION_MARKER}" \
  >/dev/null

[[ -f "${smoke_home}/config.json" ]] || die "safe first-run config was not created"
[[ ! -e "${smoke_home}/live/codex.ndjson" && ! -e "${smoke_home}/live/claude.ndjson" ]] ||
  die "smoke test unexpectedly created live-hook spool files"
[[ -z "$(find "${smoke_user_home}" -mindepth 1 -print -quit)" ]] ||
  die "Numbat inventory unexpectedly wrote to the disposable user home"

printf 'developer-preview smoke test passed: %s\n' "${archive}"
