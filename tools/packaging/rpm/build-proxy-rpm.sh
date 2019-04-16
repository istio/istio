#!/bin/bash

#set -x

# We rely on the following ENV variables:
# ISTIO_ENVOY_VERSION
# ISTIO_OUT
# ISTIO_GO
# USER_ID
# GROUP_ID

PKG_DIR="${ISTIO_GO}/tools/packaging"
WORK_DIR="$(mktemp -d)"
RPM_DIR="${WORK_DIR}/packaging/rpm/proxy"

cp -r "${PKG_DIR}" "${WORK_DIR}"
mv "${WORK_DIR}"/packaging/common/* "${RPM_DIR}"

# shellcheck disable=SC1091
source /opt/rh/devtoolset-6/enable

cd /builder || exit 1
git clone  https://github.com/istio/proxy.git istio-proxy
cd istio-proxy || exit 1
git checkout "${ISTIO_ENVOY_VERSION}"

cat "${RPM_DIR}/bazelrc" >> .bazelrc
BUILD_SCM_REVISION=$(date +%s)
sed -i "1i echo BUILD_SCM_REVISION ${BUILD_SCM_REVISION}\necho BUILD_SCM_STATUS Clean\nexit 0" tools/bazel_get_workspace_status

cd ..
tar cfz "${RPM_DIR}/istio-proxy.tar.gz" --exclude=.git istio-proxy
cd "${RPM_DIR}" || exit 1

if [ -n "${PACKAGE_VERSION}" ]; then
  sed -i "s/%global package_version .*/%global package_version ${PACKAGE_VERSION}/" istio-proxy.spec
fi
if [ -n "${PACKAGE_RELEASE}" ]; then
  sed -i "s/%global package_release .*/%global package_release ${PACKAGE_RELEASE}/" istio-proxy.spec
fi

fedpkg --release el7 local

mkdir -p "${ISTIO_OUT}/rpm"
cp -r x86_64/* "${ISTIO_OUT}/rpm"
chown -R "${USER_ID}":"${GROUP_ID}" "${ISTIO_OUT}/rpm"
