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

#
# This file will be fetched as: curl -L https://git.io/getLatestIstio | sh -
# so it should be pure bourne shell, not bash (and not reference other scripts)
#
# The script fetches the latest Istio release candidate and untars it.
# You can pass variables on the command line to download a specific version
# or to override the processor architecture. For example, to download
# Istio 1.6.8 for the x86_64 architecture and linux OS,
# run curl -L https://istio.io/downloadIstio | ISTIO_VERSION=1.6.8 TARGET_ARCH=x86_64 TARGET_OS=Linux sh -.

set -e

# Determines the operating system.
OS="${TARGET_OS:-$(uname)}"
if [ "${OS}" = "Darwin" ] ; then
  OSEXT="osx"
else
  OSEXT="linux"
fi

# Determine the latest Istio version by version number ignoring alpha, beta, and rc versions.
if [ "${ISTIO_VERSION}" = "" ] ; then
  ISTIO_VERSION="$(curl -sL https://github.com/istio/istio/releases | \
                  grep -o 'releases/[0-9]*.[0-9]*.[0-9]*/' | sort -V | \
                  tail -1 | awk -F'/' '{ print $2}')"
  ISTIO_VERSION="${ISTIO_VERSION##*/}"
fi

LOCAL_ARCH=$(uname -m)
if [ "${TARGET_ARCH}" ]; then
    LOCAL_ARCH=${TARGET_ARCH}
fi

case "${LOCAL_ARCH}" in
  x86_64|amd64)
    ISTIO_ARCH=amd64
    ;;
  armv8*|aarch64*|arm64)
    ISTIO_ARCH=arm64
    ;;
  armv*)
    ISTIO_ARCH=armv7
    ;;
  *)
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
    ;;
esac

if [ "${ISTIO_VERSION}" = "" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.0.4"
  exit 1;
fi

NAME="istio-$ISTIO_VERSION"
URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-${OSEXT}.tar.gz"
ARCH_URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"

with_arch() {
  printf "\nDownloading %s from %s ...\n" "$NAME" "$ARCH_URL"
  if ! curl -o /dev/null -sIf "$ARCH_URL"; then
    printf "\n%s is not found, please specify a valid ISTIO_VERSION and TARGET_ARCH\n" "$ARCH_URL"
    exit 1
  fi
  curl -fsLO "$ARCH_URL"
  filename="istio-${ISTIO_VERSION}-${OSEXT}-${ISTIO_ARCH}.tar.gz"
  tar -xzf "${filename}"
  rm "${filename}"
}

without_arch() {
  printf "\nDownloading %s from %s ..." "$NAME" "$URL"
  if ! curl -o /dev/null -sIf "$URL"; then
    printf "\n%s is not found, please specify a valid ISTIO_VERSION\n" "$URL"
    exit 1
  fi
  curl -fsLO "$URL"
  filename="istio-${ISTIO_VERSION}-${OSEXT}.tar.gz"
  tar -xzf "${filename}"
  rm "${filename}"
}

# Istio 1.6 and above support arch
# Istio 1.5 and below do not have arch support
ARCH_SUPPORTED="1.6"
# Istio 1.10 and above support arch for osx arm64
ARCH_SUPPORTED_OSX="1.10"

if [ "${OS}" = "Linux" ] ; then
  # This checks if ISTIO_VERSION is less than ARCH_SUPPORTED (version-sort's before it)
  if [ "$(printf '%s\n%s' "${ARCH_SUPPORTED}" "${ISTIO_VERSION}" | sort -V | head -n 1)" = "${ISTIO_VERSION}" ]; then
    without_arch
  else
    with_arch
  fi
elif [ "${OS}" = "Darwin" ] ; then
  # This checks if ISTIO_VERSION is less than ARCH_SUPPORTED_OSX (version-sort's before it) or ISTIO_ARCH not equal to arm64
  if [ "$(printf '%s\n%s' "${ARCH_SUPPORTED_OSX}" "${ISTIO_VERSION}" | sort -V | head -n 1)" = "${ISTIO_VERSION}" ] || [ "${ISTIO_ARCH}" != "arm64" ]; then
    without_arch
  else
    with_arch
  fi
else
  printf "\n\n"
  printf "Unable to download Istio %s at this moment!\n" "$ISTIO_VERSION"
  printf "Please verify the version you are trying to download.\n\n"
  exit 1
fi

printf ""
printf "\nIstio %s Download Complete!\n" "$ISTIO_VERSION"
printf "\n"
printf "Istio has been successfully downloaded into the %s folder on your system.\n" "$NAME"
printf "\n"
BINDIR="$(cd "$NAME/bin" && pwd)"
printf "Next Steps:\n"
printf "See https://istio.io/latest/docs/setup/install/ to add Istio to your Kubernetes cluster.\n"
printf "\n"
printf "To configure the istioctl client tool for your workstation,\n"
printf "add the %s directory to your environment path variable with:\n" "$BINDIR"
printf "\t export PATH=\"\$PATH:%s\"\n" "$BINDIR"
printf "\n"
printf "Begin the Istio pre-installation check by running:\n"
printf "\t istioctl x precheck \n"
printf "\n"
printf "Need more information? Visit https://istio.io/latest/docs/setup/install/ \n"
