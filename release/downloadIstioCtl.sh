#!/usr/bin/env bash
#
# Separate downloader for istioctl
#
# This file will be fetched as: curl -sL https://git.io/getLatestIstioCtl | sh -
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
                  sort -t"." -k 1,1 -k 2,2 -k 3,3 -k 4,4 | tail -n 1)
fi

if [ "x${ISTIO_VERSION}" = "x" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.0.4"
  exit;
fi

# Downloads the istioctl binary archive.
tmp=$(mktemp -d /tmp/istioctl.XXXXXX)
filename="istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
cd "$tmp" || exit
URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istioctl-${ISTIO_VERSION}-${OSEXT}.tar.gz"
printf "Downloading %s from %s ... \n" "${filename}" "${URL}"
curl -sLO "${URL}"
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