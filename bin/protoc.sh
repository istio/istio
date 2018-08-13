#!/usr/bin/env bash

if [[ $# -le 0 ]]; then
    echo Require more than one argument to protoc.
    exit 1
fi

WD="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="$(dirname "$WD")"

# Ensure expected GOPATH setup
if [ "$ROOT" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

gen_img=gcr.io/istio-testing/protoc:2018-06-12

docker run  -i  --rm --entrypoint /usr/bin/protoc -v "$ROOT":"$ROOT" -w "$(pwd)" "$gen_img" $*
