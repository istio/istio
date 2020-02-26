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

# Determines the istioctl version.
if [ "x${ISTIO_VERSION}" = "x" ] ; then
  ISTIO_VERSION=$(curl -L -s https://api.github.com/repos/istio/istio/releases | \
                  grep tag_name | sed "s/ *\"tag_name\": *\"\\(.*\\)\",*/\\1/" | \
                  grep -v -E "(alpha|beta|rc)\.[0-9]$" | sort -t"." -k 1,1 -k 2,2 -k 3,3 -k 4,4 | tail -n 1)
fi

if [ "x${ISTIO_VERSION}" = "x" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.0.4"
  exit;
fi

download_failed () {
  printf "Download failed, please make sure your ISTIO_VERSION is correct and verify the download URL exists!"
  exit 1
}

# Downloads the istioctl binary archive.
tmp=$(mktemp -d /tmp/istioctl.XXXXXX)
filename="istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
cd "$tmp" || exit
URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
printf "Downloading %s from %s ... \n" "${filename}" "${URL}"
curl -sLO "${URL}" || download_failed
printf "%s download complete!\n" "${filename}"

# setup istioctl
tar -xzf "${filename}"
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
printf "Begin the Istio pre-installation verification check by running:\n"
printf "\t istioctl verify-install \n"
printf "\n"
printf "Need more information? Visit https://istio.io/docs/reference/commands/istioctl/ \n"
