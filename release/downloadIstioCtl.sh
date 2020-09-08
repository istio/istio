#!/bin/sh

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
##############################################################################

# Separate downloader for istioctl
#
# You can fetch the the istioctl file using:
# curl -sL https://raw.githubusercontent.com/istio/istio/${BRANCH}/release/downloadIstioCtl.sh | sh -
#
# where ${BRANCH} is either your branch name (e.g. release-1.4) or master.
#

# Determines the operating system.
OS="$(uname)"
if [ "x${OS}" = "xDarwin" ] ; then
  OSEXT="osx"
else
  OSEXT="linux"
fi

# Determine the latest Istio version by version number ignoring alpha, beta, and rc versions.
if [ "x${ISTIO_VERSION}" = "x" ] ; then
  ISTIO_VERSION="$(curl -sL https://github.com/istio/istio/releases | \
                  grep -o 'releases/[0-9]*.[0-9]*.[0-9]*/' | sort --version-sort | \
                  tail -1 | awk -F'/' '{ print $2}')"
  ISTIO_VERSION="${ISTIO_VERSION##*/}"
fi

if [ "x${ISTIO_VERSION}" = "x" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.0.4"
  exit;
fi

LOCAL_ARCH=$(uname -m)
if [ "${TARGET_ARCH}" ]; then
    LOCAL_ARCH=${TARGET_ARCH}
fi

case "${LOCAL_ARCH}" in 
  x86_64)
    ISTIO_ARCH=amd64
    ;;
  armv8*)
    ISTIO_ARCH=arm64
    ;;
  aarch64*)
    ISTIO_ARCH=arm64
    ;;
  armv*)
    ISTIO_ARCH=armv7
    ;;
  amd64|arm64)
    ISTIO_ARCH=${LOCAL_ARCH}
    ;;
  *)
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
    ;;
esac

download_failed () {
  printf "Download failed, please make sure your ISTIO_VERSION is correct and verify the download URL exists!"
  exit 1
}

# Downloads the istioctl binary archive.
tmp=$(mktemp -d /tmp/istioctl.XXXXXX)
NAME="istioctl-${ISTIO_VERSION}"

cd "$tmp" || exit
URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
ARCH_URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"

with_arch() {
  printf "\nDownloading %s from %s ...\n" "${NAME}" "$ARCH_URL"
  curl -fsLO "$ARCH_URL"
  filename="istioctl-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"
  tar -xzf "${filename}"
}

without_arch() {
  printf "\n Downloading %s from %s ... \n" "${NAME}" "${URL}"
  curl -fsLO "$URL"
  filename="istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
  tar -xzf "${filename}"
}

# Istio 1.6 and above support arch
ARCH_SUPPORTED=$(echo "$ISTIO_VERSION" | awk  '{ ARCH_SUPPORTED=substr($0, 1, 3); print ARCH_SUPPORTED; }' )
# Istio 1.5 and below do not have arch support
ARCH_UNSUPPORTED="1.5"

if [ "${OS}" = "Linux" ] ; then
  # This checks if 1.6 <= 1.5 or 1.4 <= 1.5
  if [ "$(expr "${ARCH_SUPPORTED}" \<= "${ARCH_UNSUPPORTED}")" -eq 1 ]; then
    without_arch
  else
    with_arch
  fi
elif [ "x${OS}" = "xDarwin" ] ; then
  without_arch
else
  download_failed
fi

printf "%s download complete!\n" "${filename}"

# setup istioctl
cd "$HOME" || exit
mkdir -p ".istioctl/bin"
mv "${tmp}/istioctl" ".istioctl/bin/istioctl"
chmod +x ".istioctl/bin/istioctl"
rm -r "${tmp}"

# Print message
printf "\n"
printf "Add the istioctl to your path with:"
printf "\n"
printf "  export PATH=\$PATH:\$HOME/.istioctl/bin \n"
printf "\n"
printf "Begin the Istio pre-installation check by running:\n"
printf "\t istioctl x precheck \n"
printf "\n"
printf "Need more information? Visit https://istio.io/docs/reference/commands/istioctl/ \n"
