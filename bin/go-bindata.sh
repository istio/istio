#!/usr/bin/env bash

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "$SCRIPTPATH")

# Ensure expected GOPATH setup
if [ "$ROOTDIR" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

gen_img=gcr.io/istio-testing/go_generate_dependency:2018-07-26

docker run  -i --volume /var/run/docker.sock:/var/run/docker.sock \
  --rm --entrypoint go-bindata -v "$ROOTDIR:$ROOTDIR" -w "$(pwd)" $gen_img "$@"
