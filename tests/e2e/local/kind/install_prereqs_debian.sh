#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" || exit; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
source "${ROOTDIR}/common.sh"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

check_go_or_fail

check_apt_get

install_curl

install_docker

install_kubectl

install_kind

echo "Everything installed for you and you are ready to go!"
