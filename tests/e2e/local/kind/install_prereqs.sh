#!/bin/bash

case "${OSTYPE}" in
  darwin*) ./install_prereqs_macos.sh;;
  linux*)
    DISTRO="$(lsb_release -i -s)"
    case "${DISTRO}" in
      Debian|Ubuntu)
        ./install_prereqs_debian.sh;;
      *) echo "unsupported distro: ${DISTRO}" ;;
    esac;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac