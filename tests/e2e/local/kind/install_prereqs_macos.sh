#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" || exit ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_macos.sh
source "${ROOTDIR}/common_macos.sh"

check_go_or_fail

check_homebrew

echo "Update homebrew..."
brew update > /dev/null
# Give write permissions for /usr/local/bin, so that brew can create symlinks.
sudo chown -R "$USER:admin" /usr/local/bin

install_curl

install_docker

install_kubectl

install_kind

echo "Prerequisite check and installation process finishes."
