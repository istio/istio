#! /bin/sh
#
# Early version of a downloader/installer for Istio
#
# This file will be fetched as: curl -L https://git.io/getIstio | sh -
# so it should be pure bourne shell, not bash (and not reference other scripts)
#
# The script fetches the latest STABLE Istio release and untars it.
#
# release/downloadIstioCandidate.sh lets users override the ISTIO_VERSION
# and is updated more often.

# DO NOT UPDATE THIS VERSION OR SCRIPT LIGHTLY - THIS IS THE "STABLE" VERSION
ISTIO_VERSION="0.8.0"

NAME="istio-$ISTIO_VERSION"
OS="$(uname)"
if [ "x${OS}" = "xDarwin" ] ; then
  OSEXT="osx"
else
  # TODO we should check more/complain if not likely to work, etc...
  OSEXT="linux"
fi

ISTIO_HOME=~/.istio
if [ -d "$ISTIO_HOME" ]; then
  echo "You already have Istio installed."
  read -r -p "Do you want to remove $ISTIO_HOME (y/n)? " answer
  if [ "$answer" != "${answer#[Yy]}" ] ;then
      rm -rf $ISTIO_HOME
  else
      exit
  fi
fi

URL="https://github.com/istio/istio/releases/download/${ISTIO_VERSION}/istio-${ISTIO_VERSION}-${OSEXT}.tar.gz"
echo "Downloading $NAME from $URL ..."
curl -L "$URL" | tar xz
mv $NAME $ISTIO_HOME
echo "Downloaded into $ISTIO_HOME:"
ls $ISTIO_HOME
BINDIR="$(cd $ISTIO_HOME/bin; pwd)"

PATH_INJECT="export PATH=\"\$PATH:$BINDIR\""
CURRENT_SHELL=$(expr "$SHELL" : '.*/\(.*\)')
if [ "$CURRENT_SHELL" = "zsh" ]; then
  grep -q "$PATH_INJECT" ~/.zshrc || echo "\n$2" >> ~/.zshrc
elif [ "$CURRENT_SHELL" = "bash" ]; then
  if [ "$(uname)" = Darwin ]; then
    grep -q "$PATH_INJECT" ~/.bash_profile || echo "\n$PATH_INJECT" >> ~/.bash_profile
  else
    grep -q "$PATH_INJECT" ~/.bashrc || echo "\n$PATH_INJECT" >> ~/.bashrc
  fi
else
  grep -q "$PATH_INJECT" ~/.profile || echo "\n$PATH_INJECT" >> ~/.profile
fi

echo "Added $BINDIR to your path."
env $SHELL
istioctl version
