#!/bin/sh
#
# Early version of a downloader/installer for Istio
#
# This file will be fetched as: curl -L https://git.io/getLatestIstio | sh -
# so it should be pure bourne shell, not bash (and not reference other scripts)
#
# The script fetches the latest Istio release candidate and untars it.
# It's derived from ../downloadIstio.sh which is for stable releases but lets
# users do curl -L https://git.io/getLatestIstio | ISTIO_VERSION=0.3.6 sh -
# for instance to change the version fetched.

# This is the latest release candidate (matches ../istio.VERSION after basic
# sanity checks)

OS="$(uname)"
if [ "x${OS}" = "xDarwin" ] ; then
  OSEXT="osx"
else
  # TODO we should check more/complain if not likely to work, etc...
  OSEXT="linux"
fi

if [ "x${ISTIO_VERSION}" = "x" ] ; then
  ISTIO_VERSION=$(curl -L -s https://api.github.com/repos/istio/istio/releases/latest | \
                  grep tag_name | sed "s/ *\"tag_name\": *\"\\(.*\\)\",*/\\1/")
fi

if [ "x${ISTIO_VERSION}" = "x" ] ; then
  printf "Unable to get latest Istio version. Set ISTIO_VERSION env var and re-run. For example: export ISTIO_VERSION=1.0.4"
  exit;
fi

NAME="istio-$ISTIO_VERSION"
URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-${OSEXT}.tar.gz"
printf "Downloading %s from %s ..." "$NAME" "$URL"
curl -L "$URL" | tar xz 
printf ""
printf "Istio %s Download Complete!\n" "$ISTIO_VERSION"
printf "\n"
printf "Istio has been successfully downloaded into the %s folder on your system.\n" "$NAME"
printf "\n"
BINDIR="$(cd "$NAME/bin" && pwd)"
printf "Next Steps:\n"
printf "See https://istio.io/docs/setup/kubernetes/install/ to add Istio to your Kubernetes cluster.\n"
printf "\n"
printf "To configure the istioctl client tool for your workstation,\n"
printf "add the %s directory to your environment path variable with:\n" "$BINDIR"
printf "\t export PATH=\"\$PATH:%s\"\n" "$BINDIR"
printf "\n"
printf "Begin the Istio pre-installation verification check by running:\n"
printf "\t istioctl verify-install \n"
printf "\n"
printf "Need more information? Visit https://istio.io/docs/setup/kubernetes/install/ \n"