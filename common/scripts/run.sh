#!/usr/bin/env bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

export FOR_BUILD_CONTAINER=1
# shellcheck disable=SC1090,SC1091
source "${WD}/setup_env.sh"


MOUNT_SOURCE="${MOUNT_SOURCE:-${PWD}}"
MOUNT_DEST="${MOUNT_DEST:-/work}"

read -ra DOCKER_RUN_OPTIONS <<< "${DOCKER_RUN_OPTIONS:-}"

[[ -t 0 ]] && DOCKER_RUN_OPTIONS+=("-it")
[[ ${UID} -ne 0 ]] && DOCKER_RUN_OPTIONS+=(-u "${UID}:${DOCKER_GID}")

selinux_relabel() {
    if ! selinuxenabled 2>/dev/null; then
	# SELinux is not enabled, no processing
	printf "%s " "$@"
	return
    fi

    local arg volume
    volume=false
    for arg; do
	if $volume; then
	    if [[ "$arg" =~ .*:.*:.* ]]; then
		printf "%s,z " "$arg"
	    else
		printf "%s:z " "$arg"
	    fi
	    volume=false
	elif [ "$arg" = --volume ] || [ "$arg" = "-v" ]; then
	    printf "%s " "$arg"
	    # Process the next argument
	    volume=true
	else
	    printf "%s " "$arg"
	    volume=false
	fi
	if [[ "$arg" =~ ^type=bind ]]; then
	    printf -- "--mount type=bind can't be configured for SELinux\nPlease convert '%s' to --volume\n" "$arg" 1>&2
	fi
    done
}

# $CONTAINER_OPTIONS becomes an empty arg when quoted, so SC2086 is disabled for the
# following command only
# selinux_relabel's output must not be quoted, so SC2046 is disabled
# shellcheck disable=SC2046,SC2086
"${CONTAINER_CLI}" run \
    --rm \
    "${DOCKER_RUN_OPTIONS[@]}" \
    --init \
    --sig-proxy=true \
    --cap-add=SYS_ADMIN \
    $(selinux_relabel ${DOCKER_SOCKET_MOUNT:--v /var/run/docker.sock:/var/run/docker.sock}) \
    -e DOCKER_HOST=${DOCKER_SOCKET_HOST:-unix:///var/run/docker.sock} \
    $CONTAINER_OPTIONS \
    --env-file <(env | grep -v ${ENV_BLOCKLIST}) \
    -e IN_BUILD_CONTAINER=1 \
    -e TZ="${TIMEZONE:-$TZ}" \
    $(selinux_relabel \
        --volume "${MOUNT_SOURCE}:${MOUNT_DEST}" \
        --mount "type=volume,source=go,destination=/go" \
        --mount "type=volume,source=gocache,destination=/gocache" \
        --mount "type=volume,source=cache,destination=/home/.cache" \
        --mount "type=volume,source=crates,destination=/home/.cargo/registry" \
        --mount "type=volume,source=git-crates,destination=/home/.cargo/git" \
        ${CONDITIONAL_HOST_MOUNTS}) \
    -w "${MOUNT_DEST}" "${IMG}" "$@"
