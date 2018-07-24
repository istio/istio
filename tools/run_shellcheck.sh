#!/bin/sh

TOOLS_DIR="$(cd "$(dirname "${0}")" && pwd -P)"
ISTIO_ROOT="$(cd "$(dirname "${TOOLS_DIR}")" && pwd -P)"

EXCLUDES="1009,1020,1054,1056,1072,1073,1083,1090,1091,1113,1117,2001,2004,2006,2007,2009,2016,2021,2028,2034,2035,2039,2043,2046,2048,2059,2068,2086,2097,2098,2100,2119,2120,2124,2126,2128,2129,2145,2148,2155,2162,2164,2166,2181,2191,2196,2206,2209,2230,2231"

# All files ending in .sh.
SH_FILES=$( \
    find "${ISTIO_ROOT}" \
        -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*')
# All files not ending in .sh but containing a shebang.
SHEBANG_FILES=$( \
    find "${ISTIO_ROOT}" \
        -not -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*' -print0 \
        | xargs --null grep -l '^#!.*sh')

echo "${SH_FILES}" "${SHEBANG_FILES}" \
    | xargs shellcheck --exclude="${EXCLUDES}"
