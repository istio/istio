#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" || exit ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_macos.sh
source "${ROOTDIR}/common_macos.sh"

check_homebrew

echo "Update homebrew..."
brew update

install_curl

install_docker

echo "Checking virtualbox..."
if ! virtualbox --help > /dev/null; then
    echo "virtualbox is not installed. Install it from homebrew cask."
    if ! brew cask install virtualbox; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "virtualbox is installed. Checking and upgrading if a newer version exists."
    if ! brew cask reinstall --force virtualbox; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
fi

echo "Checking vagrant..."
if ! vagrant --help > /dev/null; then
    echo "vagrant is not installed. Install it from homebrew cask."
    if ! brew cask install vagrant; then
    	echo "Installation from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
else
    echo "vagrant exists. Please make sure to update it to latest version."
    vagrant version
fi

install_kubectl

echo "Prerequisite check and installation process finishes."
