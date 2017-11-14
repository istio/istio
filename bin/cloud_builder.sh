#!/bin/bash
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################

set -o errexit
set -o nounset
set -o pipefail
set -x

# This file is primarily used by cloud builder to make
# an end-to-end built of istio.  It runs this script to place the
# build artifacts in a specified output directory, then runs
# create_release_archives.sh to add tar files to the directory
# (based solely on the contents of that directory), and then
# uses store_artifacts.sh to store the build on GCR/GCS.

OUTPUT_PATH=""
# The default for PROXY_PATH (which indicates where the proxy path is located
# relative to the istio repo) is based on repo manifest that places istio at:
# go/src/istio.io/istio
# and proxy at:
# src/proxy
PROXY_PATH="../../../../src/proxy"
TAG_NAME="0.0.0"
BUILD_DEBIAN="true"
BUILD_DOCKER="true"

function usage() {
  echo "$0
    -b        opts out of building debian artifacts
    -c        opts out of building docker artifacts
    -o        path to store build artifacts
    -p        path to proxy repo (relative to istio repo, defaults to ${PROXY_PATH} ) 
    -t <tag>  tag to use (optional, defaults to ${TAG_NAME} )"
  exit 1
}

while getopts bco:p:t: arg ; do
  case "${arg}" in
    b) BUILD_DEBIAN="false";;
    c) BUILD_DOCKER="false";;
    o) OUTPUT_PATH="${OPTARG}";;
    p) PROXY_PATH="${PROXY_PATH}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${PROXY_PATH}" ]] && usage

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd $ROOT

export GOPATH="$(cd "$ROOT/../../.." && pwd)":${ROOT}/vendor
echo gopath is $GOPATH

if [ ! -d "${PROXY_PATH}" ]; then
  echo "proxy dir not detected at ${PROXY_PATH}"
  usage
fi

# Proxy has some specific requirements for Bazel's
# config (plus it's nicely places bazel in batch
# mode) so this component gets built first.

pushd "${PROXY_PATH}"

# Use this file for Cloud Builder specific settings.
# This file sets RAM sizes and also specifies batch
# mode that should shutdown bazel after each call.
echo 'Setting bazel.rc'
cp tools/bazel.rc.cloudbuilder "${HOME}/.bazelrc"
if [ "${BUILD_DEBIAN}" == "true" ]; then
  ./script/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
fi
popd

# Pilot likes to check if the source tree is 'clean'
# when it queries for version/source informatin.  Some
# other components like littering the tree so it's better
# to build pilot sooner than later.

# Pilot build expects this file to exist.  The usual
# approach of adding a symlink to the user's config file
# doesn't help when the user doesn't have one.
touch pilot/platform/kube/config

# building //... results in dirtied files:
# broker/pkg/model/config/mock_store.go
# broker/pkg/platform/kube/crd/types.go
# mixer/template/apikey/go_default_library_handler.gen.go
# mixer/template/apikey/go_default_library_tmpl.pb.go
# mixer/template/template.gen.go
bazel build //pilot/...

# bazel_to_go likes to run from dir with WORKSPACE file
./bin/bazel_to_go.py
# Remove doubly-vendorized k8s dependencies that confuse go
rm -rf vendor/k8s.io/*/vendor

# bazel_to_go.py dirties generated_files and lintconfig.json
# it's easier to ask git to restore files than add
# an option to bazel_to_go to not touch them
git checkout generated_files
git checkout lintconfig.json

pushd pilot
./bin/upload-istioctl -r -o "${OUTPUT_PATH}"
# An empty hub skips the tag and push steps.  -h "" provokes unset var error msg so using " "
if [ "${BUILD_DOCKER}" == "true" ]; then
  ./bin/push-docker -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"
fi
if [ "${BUILD_DEBIAN}" == "true" ]; then
  ./bin/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
fi
popd

pushd mixer
if [ "${BUILD_DOCKER}" == "true" ]; then
  ./bin/push-docker           -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"
fi
popd

pushd security
if [ "${BUILD_DOCKER}" == "true" ]; then
  ./bin/push-docker           -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"
fi
if [ "${BUILD_DEBIAN}" == "true" ]; then
  ./bin/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
fi
popd

# store artifacts that are used by a separate cloud builder step to generate tar files
cp istio.VERSION LICENSE README.md CONTRIBUTING.md "${OUTPUT_PATH}/"
find samples install -type f \( -name "*.yaml" -o -name "cleanup*" -o -name "*.md" \) \
  -exec cp --parents {} "${OUTPUT_PATH}" \;
find install/tools -type f -exec cp --parents {} "${OUTPUT_PATH}" \;
