#!/usr/bin/env bash

echo Starting $PWD

env

export GOPATH=$PWD

cd src/istio.io/istio
make $*

