#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

function error_exit() {
    # ${BASH_SOURCE[1]} is the file name of the caller.
    echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
    exit ${2:-1}
}

function print_block_echo(){
    echo ""
    echo "#################################"
    echo $1
    echo "#################################"
    echo ""
}

function compare_output() {
    EXPECTED=$1
    RECEIVED=$2
    USER=$3

    diff $EXPECTED $RECEIVED #&>/dev/null
    if [ $? -gt 0 ]
    then
        echo "Received product page does not match $EXPECTED for user=$USER"
        return 1
    else
        echo "Received product page matches $EXPECTED for user=$USER"
        return 0
    fi
}

function kube_inject() {
    local before=${1}
    local after=${2}
    ${ISTIOCLI} kube-inject \
		-f ${before} \
		-o ${after} \
		--hub ${PILOT_HUB} \
		--tag ${PILOT_TAG} \
		-n ${NAMESPACE}
}

function generate_istio_yaml() {
    print_block_echo "Generating istio yaml in ${1}"
    local src_dir="${ROOT}/install/kubernetes/templates"
    local dest_dir="${1}"

    mkdir -p ${dest_dir}
    cp ${src_dir}/* ${dest_dir}
    sed -i "s|image: {PILOT_HUB}/\(.*\):{PILOT_TAG}|image: ${PILOT_HUB}/\1:${PILOT_TAG}|" ${dest_dir}/istio-pilot.yaml
    sed -i "s|args: [\"discovery\", \"-v\", \"2\"]|args: ["discovery", "-v", "2", \"-a\", $NAMESPACE]|" istio-pilot.yaml
    sed -i "s|image: {PROXY_HUB}/\(.*\):{PROXY_TAG}|image: ${PILOT_HUB}/\1:${PILOT_TAG}|" ${dest_dir}/istio-ingress.yaml
    sed -i "s|image: {PROXY_HUB}/\(.*\):{PROXY_TAG}|image: ${PILOT_HUB}/\1:${PILOT_TAG}|" ${dest_dir}/istio-egress.yaml
    sed -i "s|image: {MIXER_HUB}/\(.*\):{MIXER_TAG}|image: ${MIXER_HUB}/\1:${MIXER_TAG}|" ${dest_dir}/istio-mixer.yaml
}

function generate_bookinfo_yaml() {
    print_block_echo "Generating bookinfo yaml in ${1}"
    local src_dir="${ROOT}/samples/apps/bookinfo"
    local dest_dir="${1}"

    mkdir -p ${dest_dir}
    kube_inject ${src_dir}/bookinfo.yaml ${dest_dir}/bookinfo.yaml
}

# Call the specified endpoint and compare against expected output
# Ensure the % falls within the expected range
function check_routing_rules() {
    COMMAND_INPUT="$1"
    EXPECTED_OUTPUT1="$2"
    EXPECTED_OUTPUT2="$3"
    EXPECTED_PERCENT="$4"
    MAX_LOOP=5
    routing_retry_count=1
    COMMAND_INPUT="${COMMAND_INPUT} >/tmp/routing.tmp"

    while [  $routing_retry_count -le $((MAX_LOOP)) ]; do
        v1_count=0
        v3_count=0
        for count in {1..100}
        do
            temp_var1=$(eval $COMMAND_INPUT)
            compare_output $EXPECTED_OUTPUT1 "/tmp/routing.tmp" "test-user" &>/dev/null
            if [ $? -eq 0 ]; then
                (( v1_count=v1_count+1 ))
            else
                compare_output $EXPECTED_OUTPUT2 "/tmp/routing.tmp" "test-user" &>/dev/null
                if [ $? -eq 0 ]; then
                    (( v3_count=v3_count+1 ))
                fi
            fi
        done
        echo "    v1 was hit: "$v1_count" times"
        echo "    v3 was hit: "$v3_count" times"
        echo ""

        EXPECTED_V1_PERCENT=$((100-$EXPECTED_PERCENT))
        EXPECTED_V3_PERCENT=$((100-$EXPECTED_PERCENT))
        ADJUST=5
        if [ $v1_count -lt $(($EXPECTED_V1_PERCENT-$ADJUST)) ] || [  $v3_count -gt $(($EXPECTED_V3_PERCENT+$ADJUST)) ]; then
            echo "  The routing did not meet the rule that was set, try again."
            (( routing_retry_count=routing_retry_count+1 ))
        else
            # Test passed, time to exit the loop
            routing_retry_count=100
        fi

        if [ $routing_retry_count -eq $((MAX_LOOP+1)) ]; then
            echo "Test failed"
            echo ""
            return 1
        elif [ $routing_retry_count -eq 100 ]; then
            echo "Passed test"
            echo ""
        fi
    done
    return 0
}

# Retries a command with an exponential back-off.
# The back-off base is a constant 3/2
# Options:
#   -n Maximum total attempts (0 for infinite, default 10)
#   -t Maximum time to sleep between retries (default 60)
#   -s Initial time to sleep between retries. Subsequent retries
#      subject to exponential back-off up-to the maximum time.
#      (default 5)
function retry() {
    local OPTIND OPTARG ARG
    local COUNT=10
    local SLEEP=5 MAX_SLEEP=60
    local MUL=3 DIV=2 # Exponent base multiplier and divisor
                      # (Bash doesn't do floats)

    while getopts ":n:s:t:" ARG; do
        case ${ARG} in
            n) COUNT=${OPTARG};;
            s) SLEEP=${OPTARG};;
            t) MAX_SLEEP=${OPTARG};;
            *) echo "Unrecognized argument: -${OPTARG}";;
        esac
    done

    shift $((OPTIND-1))

    # If there is no command, abort early.
    [[ ${#} -le 0 ]] && { echo "No command specified, aborting."; return 1; }

    local N=1 S=${SLEEP}  # S is the current length of sleep.
    while : ; do
        echo "${N}. Executing ${@}"
        "${@}" && { echo "Command succeeded."; return 0; }

        [[ (( COUNT -le 0 || N -lt COUNT )) ]] \
          || { echo "Command '${@}' failed ${N} times, aborting."; return 1; }

        if [[ (( S -lt MAX_SLEEP )) ]] ; then
            # Must always count full exponent due to integer rounding.
            ((S=SLEEP * (MUL ** (N-1)) / (DIV ** (N-1))))
        fi

        ((S=(S < MAX_SLEEP) ? S : MAX_SLEEP))

        echo "Command failed. Will retry in ${S} seconds."
        sleep ${S}

        ((N++))
    done
}
