#!/bin/bash

set -euo pipefail

go_version="1.26.5"
go_archive_sha256="efb87ff28af9a188d0536ef5d42e63dd52ba8263cd7344a993cc48dd11dedb6a"
link_core_upstream_repository="https://github.com/danielpaulus/go-ios.git"

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
module_directory="$(cd "${script_directory}/.." && pwd -P)"
link_core_commit="$(awk -F '\"' '/^const PinnedLinkCoreCommit = / { print $2 }' "${module_directory}/internal/linkcore/system.go")"
[[ "${link_core_commit}" =~ ^[0-9a-f]{40}$ ]] || { printf 'Invalid pinned Link Core commit\n' >&2; exit 1; }
patch_path="${module_directory}/patches/link-core-tailnet.patch"
: "${HOME:?HOME must be set}"
runtime_root="${LYO_NODUS_IOS_OTA_RUNTIME_ROOT:-${HOME}/Library/Application Support/Lyo Nodus iOS OTA}"
build_root="${runtime_root}/build"
downloads_directory="${build_root}/downloads"
toolchains_directory="${build_root}/toolchains"
sources_directory="${build_root}/sources"
cache_directory="${build_root}/cache"
output_root="${module_directory}/.build"
binary_directory="${output_root}"

fail() {
  printf 'Lyo Nodus iOS OTA build failed: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

[[ "$(uname -s)" == "Darwin" ]] || fail "macOS is required"
[[ "$(uname -m)" == "arm64" ]] || fail "Apple silicon is required"
for command_name in curl git shasum tar; do
  require_command "${command_name}"
done
[[ -f "${patch_path}" ]] || fail "downstream Link Core patch is missing"

umask 077
mkdir -p "${build_root}" "${downloads_directory}" \
  "${toolchains_directory}" "${sources_directory}" \
  "${cache_directory}/go-build" "${cache_directory}/go-mod" "${binary_directory}"
chmod 0700 "${build_root}" "${downloads_directory}" \
  "${toolchains_directory}" "${sources_directory}" \
  "${cache_directory}" "${cache_directory}/go-build" "${cache_directory}/go-mod" \
  "${binary_directory}"

staging_root="$(mktemp -d "${build_root}/.build.XXXXXX")"
cleanup() {
  case "${staging_root}" in
    "${build_root}"/.build.*) rm -rf -- "${staging_root}" ;;
  esac
}
trap cleanup EXIT INT TERM

go_archive="${downloads_directory}/go${go_version}.darwin-arm64.tar.gz"
if [[ ! -f "${go_archive}" ]]; then
  staged_archive="${staging_root}/go.tar.gz"
  curl --fail --location --silent --show-error --retry 3 \
    "https://go.dev/dl/go${go_version}.darwin-arm64.tar.gz" \
    --output "${staged_archive}"
  [[ "$(shasum -a 256 "${staged_archive}" | awk '{print $1}')" == "${go_archive_sha256}" ]] \
    || fail "downloaded Go archive has the wrong SHA-256"
  mv "${staged_archive}" "${go_archive}"
fi
[[ "$(shasum -a 256 "${go_archive}" | awk '{print $1}')" == "${go_archive_sha256}" ]] \
  || fail "cached Go archive has the wrong SHA-256"

toolchain_root="${toolchains_directory}/go${go_version}"
if [[ -e "${toolchain_root}" && ! -x "${toolchain_root}/bin/go" ]]; then
  fail "cached Go toolchain directory is incomplete: ${toolchain_root}"
fi
if [[ ! -x "${toolchain_root}/bin/go" ]]; then
  toolchain_stage="${staging_root}/toolchain"
  mkdir "${toolchain_stage}"
  tar -xzf "${go_archive}" -C "${toolchain_stage}"
  [[ -x "${toolchain_stage}/go/bin/go" ]] || fail "Go archive did not contain the toolchain"
  mv "${toolchain_stage}/go" "${toolchain_root}"
fi
go_binary="${toolchain_root}/bin/go"
[[ "$("${go_binary}" version)" == "go version go${go_version} darwin/arm64" ]] \
  || fail "cached Go toolchain has the wrong version or architecture"

patch_sha256="$(shasum -a 256 "${patch_path}" | awk '{print $1}')"
source_root="${sources_directory}/link-core-${link_core_commit}-${patch_sha256}"
if [[ -e "${source_root}" && ! -d "${source_root}/.git" ]]; then
  fail "cached Link Core source directory is incomplete: ${source_root}"
fi
if [[ ! -d "${source_root}/.git" ]]; then
  source_stage="${staging_root}/link-core"
  git init -q "${source_stage}"
  git -C "${source_stage}" remote add origin "${link_core_upstream_repository}"
  git -C "${source_stage}" fetch -q --depth=1 origin "${link_core_commit}"
  git -C "${source_stage}" checkout -q --detach FETCH_HEAD
  [[ "$(git -C "${source_stage}" rev-parse HEAD)" == "${link_core_commit}" ]] \
    || fail "Link Core checkout did not resolve to the pinned commit"
  git -C "${source_stage}" apply --unidiff-zero --check "${patch_path}"
  git -C "${source_stage}" apply --unidiff-zero "${patch_path}"
  git -C "${source_stage}" diff --check
  mv "${source_stage}" "${source_root}"
fi
[[ "$(git -C "${source_root}" rev-parse HEAD)" == "${link_core_commit}" ]] \
  || fail "cached Link Core source is not the pinned commit"
git -C "${source_root}" apply --unidiff-zero --reverse --check "${patch_path}" \
  || fail "cached Link Core source does not contain exactly the required patch"
git -C "${source_root}" diff --check

workspace_file="${output_root}/go.work"
workspace_stage="${staging_root}/workspace"
mkdir "${workspace_stage}"
(
  cd "${workspace_stage}"
  GOWORK=off "${go_binary}" work init "${module_directory}" "${source_root}"
)
mv -f "${workspace_stage}/go.work" "${workspace_file}"

staged_binary="${staging_root}/lyo-nodus-ios-ota"
ldflags="-s -w -X main.patchSHA=${patch_sha256}"
(
  cd "${module_directory}"
  GOWORK="${workspace_file}" \
    GOCACHE="${cache_directory}/go-build" \
    GOMODCACHE="${cache_directory}/go-mod" \
    "${go_binary}" build -buildvcs=false -trimpath -ldflags "${ldflags}" \
      -o "${staged_binary}" ./cmd/lyo-nodus-ios-ota
)
/usr/bin/codesign --force --sign - "${staged_binary}" >/dev/null
chmod 0755 "${staged_binary}"
mv -f "${staged_binary}" "${binary_directory}/lyo-nodus-ios-ota"

"${binary_directory}/lyo-nodus-ios-ota" version
printf 'Built %s\n' "${binary_directory}/lyo-nodus-ios-ota"

# Generated only from local, shell-quoted build paths; consumed by test.sh.
printf 'go_binary=%q\nsource_root=%q\nworkspace_file=%q\ncache_directory=%q\ncandidate_binary=%q\n' \
  "${go_binary}" "${source_root}" "${workspace_file}" "${cache_directory}" \
  "${binary_directory}/lyo-nodus-ios-ota" > "${output_root}/build-env.sh"
