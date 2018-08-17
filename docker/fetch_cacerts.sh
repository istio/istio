#!/bin/bash
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

tar -czf "${CA_CERTS}" etc/ssl/certs/ca-certificates.crt usr/share/doc/ca-certificates/copyright
