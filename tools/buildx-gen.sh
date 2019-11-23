#!/bin/bash

# Copyright 2018 Istio Authors
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
base_version="${1}"
shift
base_distribution="${1}"
shift

# Get all images. Transform from `docker.target` to `"target"`
images=$(for i in "$@"; do echo "\"${i#docker.}\""; done)
# shellcheck disable=SC2001
# shellcheck disable=SC2086
# Replace newlines with comma, so we get a comma seperated list
csv=$(echo ${images} | sed -e 's/ /, /g')

# Generate the top header. This defines a group to build all images, as well as the default args
cat <<EOF > "${config}"
group "default" {
    targets = [${csv}]
}

target "args" {
    args = {
        BASE_VERSION = "${base_version}"
        BASE_DISTRIBUTION = "${base_distribution}"
    }
}
EOF

# For each docker image, define a target to build it
for file in "$@"; do
  image=${file#docker.}
  cat <<EOF >> "${config}"
target "$image" {
    context = "${out}/${file}"
    dockerfile = "Dockerfile.$image"
    inherits = ["args"]
}
EOF
done
