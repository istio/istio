#!/bin/bash -x

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

# shellcheck disable=all

# Hello version: v2, instance: helloworld-v2-same-node-67b6b764bf-pct5v
# Hello version: v1, instance: helloworld-v1-cross-node-6fc96f99b8-8fjt8

# make sure we see both hello worlds
# and make sure the request went through the proxy

POD=$(kubectl get pod -l app=sleep -o jsonpath="{.items[0].metadata.name}")
NOT_IN_MESH_POD=$(kubectl get pod -l app=sleep2 -o jsonpath="{.items[0].metadata.name}")
NODE=$(kubectl get pod $POD -o jsonpath="{.spec.nodeName}")
ZTUNNEL_POD="$(kubectl get pods -n istio-system --field-selector spec.nodeName="$NODE" -lapp=ztunnel -o custom-columns=:.metadata.name --no-headers)"
ZTUNNEL_POD2="$(kubectl get pods -n istio-system --field-selector spec.nodeName!="$NODE" -lapp=ztunnel -o custom-columns=:.metadata.name --no-headers)"

# add all the envoy metrics



# reset counters
function reset_counters() {
    kubectl exec -it -n istio-system $ZTUNNEL_POD -c istio-proxy -- iptables-nft -t mangle -Z PREROUTING
    kubectl exec -it -n istio-system $ZTUNNEL_POD2 -c istio-proxy -- iptables-nft -t mangle -Z PREROUTING
    kubectl exec -it -n istio-system $ZTUNNEL_POD -c istio-proxy -- iptables-nft -t nat -Z PREROUTING
    kubectl exec -it -n istio-system $ZTUNNEL_POD2 -c istio-proxy -- iptables-nft -t nat -Z PREROUTING
}


function get_metrics() {
    local pod="$1"
    local direction="$2"

    case $direction in
    inztunnel)
        kubectl exec -it -n istio-system $pod -c istio-proxy -- iptables-nft -t mangle -L PREROUTING -nv|grep pistioin | grep 15008 | awk '{print $1}'
        ;;
    ztunnelloop)
        kubectl exec -it -n istio-system $pod -c istio-proxy -- iptables-nft -t nat -L OUTPUT -nv|grep REDIRECT | grep 15088 | awk '{print $1}'
        ;;
    in)
        kubectl exec -it -n istio-system $pod -c istio-proxy -- iptables-nft -t mangle -L PREROUTING -nv|grep pistioin | grep -v 15008 | awk '{print $1}'
      ;;
    out)
        kubectl exec -it -n istio-system $pod -c istio-proxy -- iptables-nft -t mangle -L PREROUTING -nv|grep pistioout | awk '{print $1}'
        ;;
    esac
}

function expect_zero(){
    local number="$1"
    if [ "$number" -ne 0 ]; then
        echo "expected zero, got $number"
        exit 1
    fi
}
function expect_not_zero(){
    local number="$1"
    if [ "$number" -eq 0 ]; then
        echo "expected not zero, got $number"
        exit 1
    fi
}

function do_curl(){
    local from="$1"
    local to="$2"

    reset_counters
    echo kubectl exec $from -c sleep -- curl --max-time 3 -s $to
    kubectl exec $from -c sleep -- curl --max-time 3 -s $to
}

# make a cross node request
do_curl $POD helloworld-v1:5000/hello
expect_not_zero $(get_metrics ZTUNNEL_POD out)
expect_zero $(get_metrics ZTUNNEL_POD in)
expect_not_zero $(get_metrics ZTUNNEL_POD2 inztunnel)

# same, but with pod ip:
do_curl $POD $(kubectl get pod -l app=helloworld,version=v1 -o jsonpath="{.items[0].status.podIP}"):5000/hello
expect_not_zero $(get_metrics ZTUNNEL_POD out)
expect_zero $(get_metrics ZTUNNEL_POD in)
expect_not_zero $(get_metrics ZTUNNEL_POD2 inztunnel)


# make same node request
do_curl $POD helloworld-v2:5000/hello
kubectl exec -it $POD -c sleep -- curl helloworld-v2:5000/hello
expect_not_zero $(get_metrics ZTUNNEL_POD out)
expect_zero $(get_metrics ZTUNNEL_POD2 inztunnel)


# same, but with pod ip:
do_curl $POD $(kubectl get pod -l app=helloworld,version=v2 -o jsonpath="{.items[0].status.podIP}"):5000/hello
expect_not_zero $(get_metrics ZTUNNEL_POD out)
expect_zero $(get_metrics ZTUNNEL_POD2 inztunnel)
expect_not_zero $(get_metrics ZTUNNEL_POD ztunnelloop)


# make cross node request
do_curl $NOT_IN_MESH_POD helloworld-v1:5000/hello
kubectl exec -it $NOT_IN_MESH_POD -c sleep -- curl helloworld-v1:5000/hello
expect_zero $(get_metrics ZTUNNEL_POD out)
expect_zero $(get_metrics ZTUNNEL_POD in)
expect_not_zero $(get_metrics ZTUNNEL_POD2 in)
expect_zero $(get_metrics ZTUNNEL_POD2 inztunnel)


# make same node request
do_curl $NOT_IN_MESH_POD helloworld-v2:5000/hello
expect_zero $(get_metrics ZTUNNEL_POD out)
expect_not_zero $(get_metrics ZTUNNEL_POD in)
expect_zero $(get_metrics ZTUNNEL_POD2 inztunnel)
expect_zero $(get_metrics ZTUNNEL_POD2 out)
expect_zero $(get_metrics ZTUNNEL_POD2 in)




echo "PASSED"
exit 0
