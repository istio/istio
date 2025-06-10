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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

set -ex

toJson () {
        python3 -c '
import sys, yaml, json
yml = list(y for y in yaml.safe_load_all(sys.stdin) if y)
if len(yml) == 1: yml = yml[0]
json.dump(yml, sys.stdout, indent=4)
'
}

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
buildx-create

HUBS="${HUBS:?specify a space separated list of hubs}"
TAG="${TAG:?specify a tag}"
defaultTargets="$(< "${ROOT}/tools/docker.yaml" toJson | toJson | jq '[.images[] | select(.base) | .name] | join(",")' -r)"
DOCKER_TARGETS="${DOCKER_TARGETS:-${defaultTargets}}"

# For multi architecture building:
# See https://medium.com/@artur.klauser/building-multi-architecture-docker-images-with-buildx-27d80f7e2408 for more info
# * docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
# * docker buildx create --name multi-arch --platform linux/amd64,linux/arm64 --use
# * export DOCKER_ARCHITECTURES="linux/amd64,linux/arm64"
# Note: if you already have a container builder before running the qemu setup you will need to restart them
"${ROOT}/tools/docker" --push --no-cache --no-clobber --targets="${DOCKER_TARGETS}"

APKO_IMAGES=""
APKO_NFT_IMAGES=""
for h in ${HUBS}; do
  for t in ${TAGS:-$TAG}; do
    APKO_IMAGES+="${h}/iptables:$t "
    APKO_NFT_IMAGES+="${h}/nftables:$t "
  done
done

# Build apko base image, which isn't part of our image building tool
APKO_ARCHES="$(echo "${DOCKER_ARCHITECTURES:-arm64,amd64}" | sed 's/linux\///g')"

# Verify the image contains only expected binaries.
dir=$(mktemp -d)
pushd "${dir}" > /dev/null
apko build "${ROOT}/docker/iptables.yaml" dummy-tag "${dir}/img.tar"
tar xf "${dir}/img.tar"
echo "write ${dir}"
for f in *.tar.gz; do
  exefiles+="$(tar tvfz "${f}" | grep '^-..x' |  awk '{print $6}')"
  exefiles+="\n"
done
unexpectedFiles="$(
  <<<"${exefiles}" grep -v '^usr/lib/xtables' | \
    # Allow all libraries - maybe we should lock down more though
    grep -v '^usr/bin/xtables' | \
    grep -v '^usr/bin/ldconfig$' | \
    grep -v '^etc/apk/commit_hooks.d/ldconfig-commit.sh$' | \
    grep -v '.*\.so[0-9\.]*' || true
)"
expectedFiles=(
  "usr/bin/xtables-legacy-multi"
)
for want in "${expectedFiles[@]}"; do
  if ! grep -q "${want}" <<<"${exefiles}"; then
    echo "Missing expected binary! ${want}"
    exit 1
  fi
done
if [[ "${unexpectedFiles}" != "" ]]; then
  echo "Found unexpected binaries: ${unexpectedFiles}"
  exit 1
fi

# Now actually build it
# shellcheck disable=SC2086
apko publish --arch="${APKO_ARCHES}" docker/iptables.yaml ${APKO_IMAGES}

# Build apko nft base image, which isn't part of our image building tool
# Verify the image contains only expected binaries.
dir=$(mktemp -d)
pushd "${dir}" > /dev/nul
apko build "${ROOT}/docker/nftables.yaml" dummy-tag "${dir}/img-nft.tar"
tar xf "${dir}/img-nft.tar"
echo "write ${dir}"
for f in *.tar.gz; do
  exefiles+="$(tar tvfz "${f}" | grep '^-..x' |  awk '{print $6}')"
  exefiles+="\n"
done
expectedFiles=(
  "usr/bin/nft"
)
for want in "${expectedFiles[@]}"; do
  if ! grep -q "${want}" <<<"${exefiles}"; then
    echo "Missing expected binary! ${want}"
    exit 1
  fi
done
# Now actually build it
# shellcheck disable=SC2086
apko publish --arch="${APKO_ARCHES}" docker/nftables.yaml ${APKO_NFT_IMAGES}
