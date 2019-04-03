#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" || exit; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

check_apt_get

install_curl

install_docker

function check_and_install_golang() {
	sudo apt-get install -y golang
}

function check_and_install_kind() {
	echo "Checking KinD is installed..."
	if ! kind --help > /dev/null; then
		if ! (go get sigs.k8s.io/kind); then
			echo "Looks like KinD installation failed."
			echo "Please install it manually then run this script again."
			exit 1
		fi
	fi
}

check_and_install_golang
check_and_install_kind

echo "Everything installed for you and ready to go."
