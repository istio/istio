#!/usr/bin/env bash

if [[ $# -le 0 ]]; then
    echo Require more than one argument to protoc.
    exit 1
fi

WD=$(dirname "$0")
WD=$(cd "$WD" && pwd)
ROOT=$(dirname "$WD")

# Ensure expected GOPATH setup
if [ "$ROOT" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

gen_img=gcr.io/istio-testing/protoc:2018-06-12

docker run  -i --volume /var/run/docker.sock:/var/run/docker.sock \
  --rm --entrypoint /usr/bin/protoc -v "$ROOT:$ROOT" -w "$(pwd)" $gen_img "$@"
