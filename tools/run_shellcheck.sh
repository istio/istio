#!/bin/sh

TOOLS_DIR="$(cd "$(dirname "${0}")" && pwd -P)"
ISTIO_ROOT="$(cd "$(dirname "${TOOLS_DIR}")" && pwd -P)"

EXCLUDES=""
EXCLUDES+="1009,"
EXCLUDES+="1020,"
EXCLUDES+="1054,"
EXCLUDES+="1056,"
EXCLUDES+="1072,"
EXCLUDES+="1073,"
EXCLUDES+="1083,"
EXCLUDES+="1090,"
EXCLUDES+="1091,"
EXCLUDES+="1113,"
EXCLUDES+="1117,"
EXCLUDES+="2001,"
EXCLUDES+="2004,"
EXCLUDES+="2006,"
EXCLUDES+="2007,"
EXCLUDES+="2009,"
EXCLUDES+="2016,"
EXCLUDES+="2021,"
EXCLUDES+="2028,"
EXCLUDES+="2034,"
EXCLUDES+="2035,"
EXCLUDES+="2039,"
EXCLUDES+="2043,"
EXCLUDES+="2046,"
EXCLUDES+="2048,"
EXCLUDES+="2059,"
EXCLUDES+="2068,"
EXCLUDES+="2086,"
EXCLUDES+="2097,"
EXCLUDES+="2098,"
EXCLUDES+="2100,"
EXCLUDES+="2119,"
EXCLUDES+="2120,"
EXCLUDES+="2124,"
EXCLUDES+="2126,"
EXCLUDES+="2128,"
EXCLUDES+="2129,"
EXCLUDES+="2145,"
EXCLUDES+="2148,"
EXCLUDES+="2155,"
EXCLUDES+="2162,"
EXCLUDES+="2164,"
EXCLUDES+="2166,"
EXCLUDES+="2181,"
EXCLUDES+="2191,"
EXCLUDES+="2196,"
EXCLUDES+="2206,"
EXCLUDES+="2209,"
EXCLUDES+="2230,"
EXCLUDES+="2231"

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
