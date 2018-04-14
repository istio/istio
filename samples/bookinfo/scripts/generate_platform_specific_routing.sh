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
    routing_dir=platforms/$platform/routing/v1alpha1
    mkdir -p $routing_dir

    for routing_spec_path in routing/v1alpha1/*; do
        routing_spec=$(basename $routing_spec_path)
        cp  $routing_spec_path $routing_dir/$routing_spec
        for service in ratings reviews productpage details; do
            sed -i.bak "s/name: $service$/service: $service$dns_domain/g" $routing_dir/$routing_spec
        done
        rm -f $routing_dir/${routing_spec}.bak
    done
}

generate_routing eureka ''
generate_routing consul '.service.consul'
