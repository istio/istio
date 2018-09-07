#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common.sh
source "${ROOTDIR}/common.sh"

case "${OSTYPE}" in
  darwin*)
    echo "We are going to install/update curl, homebrew, docker, kubectl, hyperkit, minikube in your system."
    echo "And will do `homebrew update` to fetch the latest packages."
    if read_input_y; then
      ./install_prereqs_macos.sh
    fi;;
  linux*)
    DISTRO="$(lsb_release -i -s)"
    # If lsb_release is not installed on CentOS, DISTRO will be empty.
    if [[ -z "$DISTRO" && -f /etc/centos-release ]]; then
      DISTRO="CentOS"
    fi
    case "${DISTRO}" in
      Debian|Ubuntu)
        echo "We are going to install/update curl, apt-get, dpkg, docker, kubectl, kvm2, minikube in your system."
        echo "And will do `apt-get update` to fetch the latest packages."
        if read_input_y; then
          ./install_prereqs_debian.sh
        fi;;
      CentOS)
        echo "We are going to install/update curl, yum, rpm, docker, kubectl, kvm2, minikube in your system."
        echo "And will do `yum update` to fetch the latest packages."
        if read_input_y; then
          ./install_prereqs_centos.sh
        fi;;
      *) echo "unsupported distro: ${DISTRO}" ;;
    esac;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac
