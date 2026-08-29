#!/bin/bash

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
module_directory="$(cd "${script_directory}/.." && pwd -P)"
: "${HOME:?HOME must be set}"
runtime_root="${DEPLOY_LINK_RUNTIME_ROOT:-${HOME}/Library/Application Support/Deploy Link}"
link_core_commit="3ebc297691a9e364772aef027744ebc0c49421a5"
patch_sha256="$(shasum -a 256 "${module_directory}/patches/link-core-tailnet.patch" | awk '{print $1}')"

"${script_directory}/install.sh"

go_binary="${runtime_root}/build/toolchains/go1.26.5/bin/go"
source_root="${runtime_root}/build/sources/link-core-${link_core_commit}-${patch_sha256}"
workspace_file="${runtime_root}/build/workspaces/${patch_sha256}/go.work"
go_cache="${runtime_root}/build/cache/go-build"
module_cache="${runtime_root}/build/cache/go-mod"
deploy_link_binary="${runtime_root}/bin/deploy-link"

git -C "${source_root}" apply --unidiff-zero --reverse --check "${module_directory}/patches/link-core-tailnet.patch"
git -C "${source_root}" diff --check

(
  cd "${source_root}"
  GOWORK="${workspace_file}" GOCACHE="${go_cache}" GOMODCACHE="${module_cache}" \
    "${go_binary}" test ./ios ./ios/tunnel ./ios/installationproxy ./ios/zipconduit
)
(
  cd "${module_directory}"
  GOWORK="${workspace_file}" GOCACHE="${go_cache}" GOMODCACHE="${module_cache}" \
    "${go_binary}" test ./...
  GOWORK="${workspace_file}" GOCACHE="${go_cache}" GOMODCACHE="${module_cache}" \
    "${go_binary}" test -race ./...
  GOWORK="${workspace_file}" GOCACHE="${go_cache}" GOMODCACHE="${module_cache}" \
    "${go_binary}" vet ./...
)

"${deploy_link_binary}" version
printf 'Deploy Link tests passed\n'
