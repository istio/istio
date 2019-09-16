#!/bin/bash

# Copyright Istio Authors
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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

set -o errexit
set -o nounset
set -o pipefail

# based on the following
# https://github.com/GoogleCloudPlatform/distroless/blob/master/cacerts/extract.sh

# https://packages.debian.org/buster/ca-certificates
# Latest available ca certs as of 2017-12-14
DEB_CACAERTS=http://ftp.de.debian.org/debian/pool/main/c/ca-certificates/ca-certificates_20170717_all.deb
DEB=ca-certs.deb

DEB_DIR=$(mktemp -d)

# outputs
# These files are packaged
CA_CERTS=${WD}/ca-certificates.tgz


function cleanup {
  rm -rf "${DEB_DIR}"
}

trap cleanup exit

cd "${DEB_DIR}"
curl -s ${DEB_CACAERTS} --output ${DEB}


ar -x $DEB data.tar.xz
tar -xf data.tar.xz ./usr/share/ca-certificates
tar -xf data.tar.xz ./usr/share/doc/ca-certificates/copyright

# Concat all the certs.
CERT_FILE=./etc/ssl/certs/ca-certificates.crt
mkdir -p "$(dirname $CERT_FILE)"

# concat all certs
for cert in $(find usr/share/ca-certificates -type f | sort); do
  cat "$cert" >> ${CERT_FILE}
done

tar -czf "${CA_CERTS}" --owner=0 --group=0 etc/ssl/certs/ca-certificates.crt usr/share/doc/ca-certificates/copyright
