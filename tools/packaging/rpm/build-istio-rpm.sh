#!/bin/bash

#set -x

# We rely on the following ENV variables:
# ISTIO_GO
# ISTIO_OUT
# USER_ID
# GROUP_ID

PKG_DIR="${ISTIO_GO}/tools/packaging"
WORK_DIR="$(mktemp -d)"

cp -r "${PKG_DIR}" "${WORK_DIR}"
mv "${WORK_DIR}"/packaging/common/* "${WORK_DIR}"/packaging/rpm/istio

cd "${ISTIO_GO}/.." || exit 1
tar cfz "${WORK_DIR}/packaging/rpm/istio/istio.tar.gz" --exclude=.git istio

cd "${WORK_DIR}/packaging/rpm/istio" || exit 1

if [ -n "${PACKAGE_VERSION}" ]; then
  sed -i "s/%global package_version .*/%global package_version ${PACKAGE_VERSION}/" istio.spec
fi
if [ -n "${PACKAGE_RELEASE}" ]; then
  sed -i "s/%global package_release .*/%global package_release ${PACKAGE_RELEASE}/" istio.spec
fi

fedpkg --release el7 local

mkdir -p "${ISTIO_OUT}/rpm"
cp -r x86_64/* "${ISTIO_OUT}/rpm"
chown -R "${USER_ID}":"${GROUP_ID}" "${ISTIO_OUT}/rpm"
