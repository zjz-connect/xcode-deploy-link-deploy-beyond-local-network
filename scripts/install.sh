#!/bin/bash
set -euo pipefail
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
module_directory="$(cd "${script_directory}/.." && pwd -P)"
: "${HOME:?HOME must be set}"
"${script_directory}/build.sh"
umask 077
binary_directory="${HOME}/.local/bin"
mkdir -p "${binary_directory}"
staged_binary="$(mktemp "${binary_directory}/.lyo-nodus-ios-ota.XXXXXX")"
trap 'rm -f -- "${staged_binary}"' EXIT
cp "${module_directory}/.build/lyo-nodus-ios-ota" "${staged_binary}"
chmod 0755 "${staged_binary}"
mv -f "${staged_binary}" "${binary_directory}/lyo-nodus-ios-ota"
printf 'Installed %s; service activation is separate\n' "${binary_directory}/lyo-nodus-ios-ota"
