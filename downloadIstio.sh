#! /bin/sh
#
# Early version of a downloader/installer for Istio
#
# This file will be fetched as: curl -L https://git.io/getIstio | sh -
# so it should be pure bourne shell, not bash
#
# The script fetches the latest Istio release and untars it.

# TODO: Automate updating me.
ISTIO_VERSION=${ISTIO_VERSION:-0.2.4}

NAME="istio-$ISTIO_VERSION"
OS="$(uname)"
if [ "x${OS}" = "xDarwin" ] ; then
  OSEXT="osx"
else
  # TODO we should check more/complain if not likely to work, etc...
  OSEXT="linux"
fi
ISTIO_BASE_URL=${BASE_URL:-https://github.com/istio/istio/releases/download}
URL="${ISTIO_BASE_URL}/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-${OSEXT}.tar.gz"
echo "Downloading $NAME from $URL ..."
curl -L "$URL" | tar xz

# Current URL for the debian files artifacts. Will be replaced by a proper apt repo.
DEBURL=http://gcsweb.istio.io/gcs/istio-release/releases/${ISTIO_VERSION}/deb
curl -L ${DEBURL}/istio-agent-release.deb > istio-${ISTIO_VERSION}/istio-agent-release.deb
curl -L ${DEBURL}/istio-auth-node-agent-release.deb > istio-${ISTIO_VERSION}/istio-auth-node-agent-release.deb
curl -L ${DEBURL}/istio-proxy-release.deb > istio-${ISTIO_VERSION}/istio-proxy-release.deb

# TODO: change this so the version is in the tgz/directory name (users trying multiple versions)
echo "Downloaded into $NAME:"
ls $NAME
BINDIR="$(cd $NAME/bin; pwd)"
echo "Add $BINDIR to your path; e.g copy paste in your shell and/or ~/.profile:"
echo "export PATH=\"\$PATH:$BINDIR\""
