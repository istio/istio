#!/bin/bash

case "${OSTYPE}" in
  darwin*) sh install_prereqs_macos.sh;;
  linux*)
    DISTRO="$(lsb_release -i -s)"
    case "${DISTRO}" in
      Debian|Ubuntu)
        sh install_prereqs_debian.sh;;
      *) echo "unsupported distro: ${DISTRO}" ;;
    esac;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac
