#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

set -eux

pkill dockerd
cat <<EOF > /etc/docker/daemon.json
{
  "max-concurrent-uploads": 1
}
EOF

daemon -U -- dockerd

echo "Waiting for dockerd to start..."
while :
do
  echo "Checking for running docker daemon."
  if [[ $(docker info > /dev/null 2>&1) -eq 0 ]]; then
    echo "The docker daemon is running."
    break
  fi
  sleep 1
done


DRY_RUN=true "${ROOT}"/prow/release-commit.sh
