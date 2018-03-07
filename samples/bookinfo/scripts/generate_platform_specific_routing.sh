#!/bin/bash
#
# Copyright 2018 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -o errexit

function generate_routing {
    platform=$1
    dns_domain=$2

    mkdir -p platforms/$platform/routing

    pushd routing
    for routing_spec in *; do
        cp  $routing_spec ../platforms/$platform/routing/$routing_spec
        for service in ratings reviews productpage details; do
            sed -i.bak "s/name: $service$/service: $service$dns_domain/g" ../platforms/$platform/routing/$routing_spec
        done
        rm ../platforms/$platform/routing/${routing_spec}.bak
    done
    popd
}

generate_routing eureka ''
generate_routing consul '.service.consul'
