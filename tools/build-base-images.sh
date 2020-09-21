#!/usr/bin/env bash

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

# This script runs go tests in a package, but each test is run individually. This helps
# isolate tests that are improperly depending on global state modification of other tests

set -ex

HUB="${HUB:-istio.io/docker}"
TAG="${TAG:?specify a tag}"

DOCKER_TARGETS="docker.base docker.distroless docker.app_sidecar_base_debian_9 docker.app_sidecar_base_debian_10 docker.app_sidecar_base_ubuntu_xenial docker.app_sidecar_base_ubuntu_bionic docker.app_sidecar_base_ubuntu_focal docker.app_sidecar_base_centos_8" make dockerx.pushx
