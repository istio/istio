#!/bin/bash

case "${OSTYPE}" in
  linux*)
    DISTRO="$(lsb_release -i -s)"
    case "${DISTRO}" in
      Debian|Ubuntu)
        ./install_prereqs_debian.sh;;
      *) echo "unsupported distro: ${DISTRO}" ;;
    esac;;
  darwin*)
    ./install_prereqs_macos.sh;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac
