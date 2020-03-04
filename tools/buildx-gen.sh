#!/bin/bash

# Copyright 2019 Istio Authors
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

set -eu

out="${1}"
config="${out}/docker-bake.hcl"
shift

variants=\"$(for i in ${DOCKER_ALL_VARIANTS}; do echo "\"${i}\""; done | xargs | sed -e 's/ /\", \"/g')\"
cat <<EOF > "${config}"
group "all" {
    targets = [${variants}]
}
EOF

# Generate the top header. This defines a group to build all images for each variant
for variant in ${DOCKER_ALL_VARIANTS}; do
  # Get all images. Transform from `docker.target` to `"target"` as a comma seperated list
  images=\"$(for i in "$@"; do echo "\"${i#docker.}-${variant}\""; done | xargs | sed -e 's/ /\", \"/g')\"
  cat <<EOF >> "${config}"
group "${variant}" {
    targets = [${images}]
}
EOF
done

# For each docker image, define a target to build it
for file in "$@"; do
  for variant in ${DOCKER_ALL_VARIANTS}; do
    image=${file#docker.}
    tag="${TAG}"
    # The default variant has no suffix, others do
    if [[ "${variant}" != "default" ]]; then
      tag+="-${variant}"
    fi

    # Output locally (like `docker build`) by default, or push
    # Push requires using container driver. See https://github.com/docker/buildx#working-with-builder-instances
    output='output = ["type=docker"]'
    if [[ -n "${DOCKERX_PUSH:-}" ]]; then
      output='output = ["type=registry"]'
    fi

    cat <<EOF >> "${config}"
target "$image-$variant" {
    context = "${out}/${file}"
    dockerfile = "Dockerfile.$image"
    tags = ["${HUB}/${image}:${tag}"]
    args = {
      BASE_VERSION = "${BASE_VERSION}"
      BASE_DISTRIBUTION = "${variant}"
      proxy_version = "istio-proxy:${PROXY_REPO_SHA}"
      istio_version = "${VERSION}"
    }
    ${output}
}
EOF
    # For the default variant, create an alias so we can do things like `build pilot` instead of `build pilot-default`
    if [[ "${variant}" == "default" ]]; then
    cat <<EOF >> "${config}"
target "$image" {
    inherits = ["$image-$variant"]
}
EOF
    fi
  done
done
