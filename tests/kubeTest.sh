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

# Local vars
SCRIPT_DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
EXAMPLES_DIR=$SCRIPT_DIR/apps/bookinfo/output
FAILURE_COUNT=0
TEAR_DOWN=true
TEST_DIR="$(mktemp -d /tmp/kubetest.XXXXX)"
ISTIO_INSTALL_DIR="${TEST_DIR}/istio"
BOOKINFO_DIR="${TEST_DIR}/bookinfo"
WRK_URL="https://storage.googleapis.com/istio-build-deps/wrk-linux"

# Import relevant utils
. ${SCRIPT_DIR}/kubeUtils.sh || \
  { echo 'could not load k8s utilities'; exit 1; }
. ${SCRIPT_DIR}/istioUtils.sh || error_exit 'Could not load istio utilities'

. ${ROOT}/istio.VERSION || error_exit "Could not source versions"

RULES_DIR="${ROOT}/samples/apps/bookinfo/rules"

while getopts :c:i:sn:m:x: arg; do
  case ${arg} in
    c) ISTIOCLI="${OPTARG}";;
    i) ISTIOCTL_URL="${OPTARG}";;
    s) TEAR_DOWN=false;;
    n) NAMESPACE="${OPTARG}";;
    m) PILOT_HUB_TAG="${OPTARG}";; # Format: "<hub>,<tag>"
    x) MIXER_HUB_TAG="${OPTARG}";; # Format: "<hub>,<tag>"
    *) error_exit "Unrecognized argument -${OPTARG}";;
  esac
done

[[ -z ${NAMESPACE} ]] && NAMESPACE="$(generate_namespace)"

if [[ -z ${ISTIOCLI} ]]; then
    echo "Downloading istioctl from ${ISTIOCTL_URL}/istioctl-linux"
    wget -O "${TEST_DIR}/istioctl" "${ISTIOCTL_URL}/istioctl-linux" || error_exit "Could not download istioctl"
    chmod +x "${TEST_DIR}/istioctl"
    ISTIOCLI="${TEST_DIR}/istioctl -c ${HOME}/.kube/config"
fi

if [[ -z ${WRK} ]]; then
    wget -q -O "${TEST_DIR}/wrk" "${WRK_URL}" || error_exit "Could not download wrk"
    chmod +x "${TEST_DIR}/wrk"
    WRK="${TEST_DIR}/wrk"
fi

if [[ -n ${PILOT_HUB_TAG} ]]; then
    PILOT_HUB="$(echo ${PILOT_HUB_TAG}|cut -f1 -d,)"
    PILOT_TAG="$(echo ${PILOT_HUB_TAG}|cut -f2 -d,)"
fi

if [[ -n ${MIXER_HUB_TAG} ]]; then
    MIXER_HUB="$(echo ${MIXER_HUB_TAG}|cut -f1 -d,)"
    MIXER_TAG="$(echo ${MIXER_HUB_TAG}|cut -f2 -d,)"
fi

function tear_down {
    [[ ${TEAR_DOWN} == false ]] && exit 0
    # Teardown
    cleanup_all_rules
    cleanup
    rm -rf ${TEST_DIR}
}

trap tear_down EXIT

# Setup
create_namespace
generate_istio_yaml "${ISTIO_INSTALL_DIR}"
deploy_istio "${ISTIO_INSTALL_DIR}"
setup_mixer
generate_bookinfo_yaml "${BOOKINFO_DIR}"
deploy_bookinfo "${BOOKINFO_DIR}"; URL=$GATEWAY_URL

# Verify default routes
print_block_echo "Testing default route behavior on ${URL} ..."
for (( i=0; i<=4; i++ ))
do
    response=$(curl --write-out %{http_code} --silent --output /dev/null ${URL}/productpage)
    if [ $response -ne 200 ]
    then
        if [ $i -eq 4 ]
        then
            ((FAILURE_COUNT++))
            dump_debug
            error_exit 'Failed to resolve default routes'
        fi
        echo "Couldn't get to the bookinfo product page, trying again...'"
    else
        echo "Success!"
        break
    fi
    sleep 60
done

# Test version routing
print_block_echo "Testing version routing..."
create_rule $RULES_DIR/route-rule-all-v1.yaml
create_rule $RULES_DIR/route-rule-reviews-test-v2.yaml
echo "Waiting for rules to propagate..."
sleep 30

function test_version_routing_response() {
    USER=$1
    VERSION=$2
    echo "injecting traffic for user=$USER, expecting productpage-$USER-$VERSION..."
    curl -s -b "foo=bar;user=$USER;" ${URL}/productpage > /tmp/productpage-$USER-$VERSION.html
    compare_output $EXAMPLES_DIR/productpage-$USER-$VERSION.html /tmp/productpage-$USER-$VERSION.html $USER
    if [ $? -ne 0 ]
    then
        ((FAILURE_COUNT++))
        dump_debug
    fi
}

test_version_routing_response "normal-user" "v1"
test_version_routing_response "test-user" "v2"

# Test fault injection
print_block_echo "Testing fault injection..."

create_rule $RULES_DIR/route-rule-ratings-test-delay.yaml

function test_fault_delay() {
    USER=$1
    VERSION=$2
    EXP_MIN_DELAY=$3
    EXP_MAX_DELAY=$4

    for (( i=0; i<=4; i++ ))
    do
        echo "injecting traffic for user=$USER, expecting productpage-$USER-$VERSION in $EXP_MIN_DELAY to $EXP_MAX_DELAY seconds"
        before=$(date +"%s")
        curl -s -b "foo=bar;user=$USER;" ${URL}/productpage > /tmp/productpage-$USER-$VERSION.html
        after=$(date +"%s")
        delta=$(($after-$before))
        if [ $delta -ge $EXP_MIN_DELAY ] && [ $delta -le $EXP_MAX_DELAY ]
        then
            echo "Success!"
            if [ $EXP_MIN_DELAY -gt 0 ]
            then
                compare_output $EXAMPLES_DIR/productpage-$USER-$VERSION-review-timeout.html /tmp/productpage-$USER-$VERSION.html $USER
            else
                compare_output $EXAMPLES_DIR/productpage-$USER-$VERSION.html /tmp/productpage-$USER-$VERSION.html $USER
            fi
            return 0
        elif [ $i -eq 4 ]
        then
            echo "Productpage took $delta seconds to respond (expected between $EXP_MIN_DELAY and $EXP_MAX_DELAY) for user=$USER in fault injection phase"
            ((FAILURE_COUNT++))
            dump_debug
        fi
        sleep 10
    done
    return 1
}

test_fault_delay "normal-user" "v1" 0 2
test_fault_delay "test-user" "v1" 5 8

# Remove fault injection and verify
print_block_echo "Deleting fault injection..."

delete_rule $RULES_DIR/route-rule-ratings-test-delay.yaml
echo "Waiting for rule clean up to propagate..."
sleep 30
test_fault_delay "test-user" "v2" 0 2
if [ $? -eq 0 ]
then
    echo "Fault injection was successfully cleared up"
else
    echo "Fault injection persisted"
    ((FAILURE_COUNT++))
    dump_debug
fi

# Test gradual migration traffic to reviews:v3 for all users
cleanup_all_rules
print_block_echo "Testing gradual migration..."

COMMAND_INPUT="curl -s -b 'foo=bar;user=normal-user;' ${URL}/productpage"
EXPECTED_OUTPUT1="$EXAMPLES_DIR/productpage-normal-user-v1.html"
EXPECTED_OUTPUT2="$EXAMPLES_DIR/productpage-normal-user-v3.html"
replace_rule $RULES_DIR/route-rule-reviews-50-v3.yaml
echo "Waiting for rules to propagate..."
sleep 30
echo "Expected percentage based routing is 50% to v1 and 50% to v3."

# Validate that 50% of traffic is routing to v1
# Curl the health check and check the version cookie
check_routing_rules "$COMMAND_INPUT" "$EXPECTED_OUTPUT1" "$EXPECTED_OUTPUT2" 50
if [ $? -ne 0 ]
then
    ((FAILURE_COUNT++))
    dump_debug
fi
# mixer tests
METRICS_URL=${ISTIO_MIXER_METRICS}/metrics
curl --connect-timeout 5 ${METRICS_URL}
${WRK} -t1 -c1 -d10s --latency -s ${SCRIPT_DIR}/wrk.lua ${URL}/productpage
curl --connect-timeout 5 ${METRICS_URL}
${WRK} -t2 -c2 -d10s --latency -s ${SCRIPT_DIR}/wrk.lua ${URL}/productpage
curl --connect-timeout 5 ${METRICS_URL}


if [ ${FAILURE_COUNT} -gt 0 ]
then
    echo "${FAILURE_COUNT} TESTS HAVE FAILED"
    exit 1
else
    echo "TESTS HAVE PASSED"
fi

cleanup_istioctl
