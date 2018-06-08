#!/bin/sh

TOOLS_DIR="$(cd "$(dirname "${0}")" && pwd -P)"
ISTIO_ROOT="$(cd "$(dirname "${TOOLS_DIR}")" && pwd -P)"

SH_FILES=$( \
    find "${ISTIO_ROOT}" \
        -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*')
SHEBANG_FILES=$( \
    find "${ISTIO_ROOT}" \
        -not -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*' -print0 \
        | xargs --null grep -l '^#!.*sh')

echo "${SH_FILES}" "${SHEBANG_FILES}" | xargs shellcheck
