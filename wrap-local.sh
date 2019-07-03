#!/bin/bash
set -exuo pipefail

cd ~/go/src/istio.io
export DOCKER_HUB=gcr.io/peripli

function clone_dependencies() {
  for dependency in "tools" "api" "proxy"; do
    if [ ! -d "$dependency" ]; then
      echo "Adding missing repo: istio.io/$dependency."
      git clone "https://github.com/istio/$dependency.git"
    fi
  done
}

clone_dependencies

cd istio
release/gcb/local.sh