#!/usr/bin/env bash

# Copyright 2020 Istio Authors
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

if [ -d ../go-control-plane ]; then
  exit 0
fi

GIT_SCHEME="sso://"
if [ -n "$PROW_JOB_ID" ]; then
  # TODO restore original git config, but this script shouldn't be in use long
  echo "Warning: editing global git config"
  GIT_SCHEME="https://"
  git config --global user.name ci-robot
  git config --global user.email ci-robot@k8s.io
  git config --global http.cookiefile /secrets/cookiefile/cookies
elif [ "$1" != "-y" ]; then
  while true; do
      echo "Warning: if you're running this with make, it will fail."
      echo "         In that case, just run ./clone-go-control-plane.sh directly, and"
      echo
      echo "         export CONDITIONAL_HOST_MOUNTS=\"--mount type=bind,source=\$(cd ../go-control-plane && pwd),destination=/go-control-plane\""
      echo
      echo "         before running make"

      read -rp "Do you want to clone into ../go-control-plane to satisfy replace directive? " yn
      case $yn in
          [Yy]* ) break;;
          [Nn]* ) exit;;
          * ) echo "Please answer yes or no.";;
      esac
  done
fi

git clone "$GIT_SCHEME"gke-internal.googlesource.com/istio/istio/ --single-branch --branch gke-dev-landow/ambient-go-control-plane ../go-control-plane