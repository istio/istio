#!/usr/bin/env bash

# Copyright 2020 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

VERSION=${VERSION:-$(date -Is)}

OLDDIR=$(pwd)
REPO=$(git rev-parse --show-toplevel)
if [[ ${REPO} == "" ]]; then
  echo "Cannot run script -- must be run from tools/bug-report in the istio repo" >&2
  exit 1
fi

TMPDIR=$(mktemp -d)
trap '{ rm -rf $TMPDIR; }' EXIT

# Build images
declare -a LGOOS=("linux" "darwin")
declare -a LGOARCH=("386" "arm" "amd64")

pushd "${REPO}" || exit
for os in "${LGOOS[@]}"; do
  for arch in "${LGOARCH[@]}"; do
    # Creates binaries in format `bug-report_linux_amd64
    FILE="${TMPDIR}/out/bug-report_${os}_${arch}-${VERSION}"
    GOOS=${os} GOARCH=${arch} go build -o "${FILE}" tools/bug-report/main.go
    if [[ -f "${FILE}" ]]; then
      chmod +x "${FILE}"
    fi
  done
done
popd ||exit

cat > "${TMPDIR}/out/README" <<EOF
Bug report binaries.

To upload to the official release repo, please run:
gsutil cp out/bug-report_* gs://gke-release/asm/

EOF

cd "${TMPDIR}" || exit
tar czvf "${OLDDIR}/bug-report-binaries.tgz" out/

