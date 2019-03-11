#!/usr/bin/env bash

echo Starting $PWD

export GOPATH=$PWD

cd src/istio.io/istio
make $*

