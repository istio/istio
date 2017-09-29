#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -ex

# customize for development / debug setup
HUB=docker.io/$(whoami)
TAG=test
NAMESPACE=prepare-proxy-test0

PLATFORM=gcloud
GCLOUD_CLUSTER_NAME=c1
GCLOUD_CLUSTER_ZONE=us-central1-a

ENVOY_UID=1337
ENVOY_PORT=80
SERVER_PORT=${ENVOY_PORT}
CLIENT_PORT=81
OTHER_PORT=82

function buildImages() {
    if [[ "$HUB" =~ ^gcr\.io ]]; then
        gcloud docker --authorize-only
    fi

    for image in app init proxy; do
        bazel run //docker:${image}_debug
        docker tag istio/docker:${image}_debug $HUB/$image:$TAG
        docker push $HUB/$image:$TAG
    done
}

function kc() {
    kubectl -n ${NAMESPACE} "$@"
}

function die() {
    echo "$@"
    exit 1
}

function waitDeploymentReady {
    name=${1}
    while true; do
	echo "waiting for all ${name} replicas to be up"
	sleep 1
	read WANT HAVE < <( \
			    kc get deployment ${name} \
			       -o go-template='{{.spec.replicas}} {{.status.replicas}}{{"\n"}}'
	)
	if [ -n "${WANT}" -a -n "${HAVE}" -a "${WANT}" == "${HAVE}" ]; then
	    break
	fi
	echo "want ${WANT}, found ${HAVE}"
    done
}

# Return the kubernetes service and pod IP ranges as a comma seperated
# list, e.g. 10.0.0.1/32,10.2.0.1/16.
function k8sClusterAndServiceIPRange() {
    case ${PLATFORM} in
        gcloud)
            gcloud container clusters describe ${GCLOUD_CLUSTER_NAME} --zone=${GCLOUD_CLUSTER_ZONE} |
	               grep -e clusterIpv4Cidr -e servicesIpv4Cidr |
	               cut -f2 -d' ' | paste -sd ","
            ;;
        *)
            echo ""
    esac
}

function redirectedPackets() {
    kc exec ${SERVER} -c init -- iptables -t nat -S ISTIO_REDIRECT -v  | \
	grep -- '--comment "istio/redirect-to-envoy-port' | \
	sed 's/.*-c \([0-9]*\).*/\1/'
}

function resetRedirected() {
    prev=$(redirectedPackets)
}

function assertRedirected {
    want=$1
    current=$(redirectedPackets)
    got=$((${prev} < ${current}))
    if [ ${want} != ${got} ]; then
	echo "test failed: got $got want $want"
	exit 1
    fi
    prev=${current}
}

TEST_ITER=0
function runTest() {
    TEST_ITER=$((${TEST_ITER} + 1))
    TEST_ITER_LABEL=iter${TEST_ITER}

    kubectl get namespace ${NAMESPACE} || kubectl create namespace ${NAMESPACE}

    # clean-up deployments from previous test iteration
    kc delete deployment --all 2>/dev/null

    cat docker/prepare_proxy_test.yaml |
        sed -e "s|TEMPLATE_HUB|${HUB}|" \
	    -e "s|TEMPLATE_TAG|${TAG}|" \
            -e "s|TEMPLATE_ITER|${TEST_ITER_LABEL}|" \
	    -e "s|CLIENT_PORT|${CLIENT_PORT}|" \
	    -e "s|SERVER_PORT|${SERVER_PORT}|" |
        kc apply -f -

    waitDeploymentReady client
    waitDeploymentReady server

    # Get specific client and server pod name and IP address.
    CLIENT=
    while [ "$CLIENT" = "" ]; do CLIENT=$(kc get pod -l app=client,iter="${TEST_ITER_LABEL}" -o jsonpath='{.items[0].metadata.name}'); done
    CLIENT_IP=
    while [ "$CLIENT_IP" = "" ]; do CLIENT_IP=$(kc get pod -l app=client,iter="${TEST_ITER_LABEL}" -o jsonpath='{.items[0].status.podIP}'); done
    SERVER=
    while [ "$SERVER" = "" ]; do SERVER=$(kc get pod -l app=server,iter="${TEST_ITER_LABEL}" -o jsonpath='{.items[0].metadata.name}'); done
    SERVER_IP=
    while [ "$SERVER_IP" = "" ]; do SERVER_IP=$(kc get pod -l app=server,iter="${TEST_ITER_LABEL}" -o jsonpath='{.items[0].status.podIP}'); done

    if [ "${TEST_IP_RANGE_INCLUDE}" = 1 ]; then
        # Only redirect service and pod traffic to Envoy.
        INCLUDE_IP_RANGE=$(k8sClusterAndServiceIPRange)
        kc exec ${SERVER} -c init -- \
           /usr/local/bin/prepare_proxy.sh -u ${ENVOY_UID} -p ${ENVOY_PORT} -i ${INCLUDE_IP_RANGE}
    else
        # redirect all outbound traffic to Envoy.
        kc exec ${SERVER} -c init -- \
           /usr/local/bin/prepare_proxy.sh -u ${ENVOY_UID} -p ${ENVOY_PORT}
    fi

    resetRedirected

    # client to server via proxy
    kc exec ${CLIENT} -c app -- curl -s ${SERVER_IP}:${SERVER_PORT} |
        grep ${SERVER_PORT} ||
        die "client => server failed"
    assertRedirected 1

    # client to server via proxy with port different than server to
    # double-check redirection
    kc exec ${CLIENT} -c app -- curl -s ${SERVER_IP}:${OTHER_PORT} |
        grep ${OTHER_PORT} ||
        die "client => server (alt) failed"
    assertRedirected 1

    # server to client via app. Should redirect to server proxy and fail
    # because server isn't listening on port ${CLIENT_PORT}.
    kc exec ${SERVER} -c app -- curl -s ${CLIENT_IP}:${CLIENT_PORT} |
        grep ServicePort=${CLIENT_PORT} &&
        die "server => client from app didn't fail"
    assertRedirected 1

    # server to client service VIP from app. should redirect
    kc exec ${SERVER} -c app -- curl -s client:${CLIENT_PORT} |
        grep ServicePort=${CLIENT_PORT} &&
        die "server => client VIP from proxy didn't fail"
    assertRedirected 1

    # server to client from proxy container. Should bypass proxy.
    kc exec ${SERVER} -c proxy -- curl -s ${CLIENT_IP}:${CLIENT_PORT} |
        grep ServicePort=${CLIENT_PORT} ||
        die "server => client from proxy failed"
    assertRedirected 0

    # server to client service VIP from proxy. should bypass proxy
    kc exec ${SERVER} -c proxy -- curl -s client:${CLIENT_PORT} |
        grep ServicePort=${CLIENT_PORT} ||
        die "server => client VIP from from proxy failed"
    assertRedirected 0

    # server app to itself via localhost - bypasses proxy
    kc exec ${SERVER} -c app -- curl -s localhost:${SERVER_PORT} |
        grep ${SERVER_PORT} ||
        die "server => server via localhost failed"
    assertRedirected 0

    # server app to itself on alternate port - bypasses proxy and should
    # fail
    kc exec ${SERVER} -c app -- curl -s localhost:${OTHER_PORT} |
        grep ${OTHER_PORT} &&
        die "server => server (alt) via localhost failed"
    assertRedirected 0

    # server app to itself via external IP address - should redirect
    # through proxy
    kc exec ${SERVER} -c app -- curl -s ${SERVER_IP}:${SERVER_PORT} |
        grep ${SERVER_PORT} ||
        die "server => server via endpoint ip failed"
    assertRedirected 1

    # server app to itself via external IP address and alternate port -
    # should redirect through proxy
    kc exec ${SERVER} -c app -- curl -s ${SERVER_IP}:${OTHER_PORT} |
        grep ${OTHER_PORT} ||
        die "server => server (alt) via endpoint ip failed"
    assertRedirected 1

    # server app to itself via external IP address - should redirect
    # through proxy
    kc exec ${SERVER} -c app -- curl -s server:${SERVER_PORT} |
        grep ${SERVER_PORT} ||
        die "server => server via VIP failed"
    assertRedirected 1

    # server app to itself via external IP address and alternate port - should redirect through proxy
    kc exec ${SERVER} -c app -- curl -s server:${OTHER_PORT} |
        grep ${OTHER_PORT} ||
        die "server => server (alt) via VIP failed"
    assertRedirected 1

    if [ "${TEST_IP_RANGE_INCLUDE}" = 1 ]; then
        # server app to external address from app
        kc exec ${SERVER} -c app -t -- curl -sI http://httpbin.org/status/418 |
            grep TEAPOT ||
            die "server => external from app failed"
        assertRedirected 0

        # server app to external address from proxy
        kc exec ${SERVER} -c proxy -t -- curl -sI http://httpbin.org/status/418 |
            grep TEAPOT ||
            die "server => external from proxy failed"
        assertRedirected 0
    fi
}

buildImages

TEST_IP_RANGE_INCLUDE=0
runTest

TEST_IP_RANGE_INCLUDE=1
runTest
