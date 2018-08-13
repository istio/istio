#!/usr/bin/env bash

WD="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="$(dirname "$WD")"

# Ensure expected GOPATH setup
if [ "$ROOT" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

gen_img=gcr.io/istio-testing/go_generate_dependency:2018-07-26

docker run  -i -e GOPATH=/go:$GOPATH --rm --entrypoint counterfeiter -v "$ROOT":"$ROOT" -w "$(pwd)" "$gen_img" $*


