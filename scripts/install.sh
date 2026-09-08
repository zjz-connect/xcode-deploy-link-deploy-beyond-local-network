#!/bin/bash
set -euo pipefail
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
module_directory="$(cd "${script_directory}/.." && pwd -P)"
: "${HOME:?HOME must be set}"
runtime_root="${NODUS_REMOTE_DEPLOY_RUNTIME_ROOT:-${HOME}/Library/Application Support/Nodus Remote Deploy}"
"${script_directory}/build.sh"
umask 077
mkdir -p "${runtime_root}/bin"
staged_binary="$(mktemp "${runtime_root}/bin/.nodus-remote-deploy.XXXXXX")"
trap 'rm -f -- "${staged_binary}"' EXIT
cp "${module_directory}/.build/nodus-remote-deploy" "${staged_binary}"
chmod 0755 "${staged_binary}"
mv -f "${staged_binary}" "${runtime_root}/bin/nodus-remote-deploy"
printf 'Installed %s; service activation is separate\n' "${runtime_root}/bin/nodus-remote-deploy"
