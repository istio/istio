#!/bin/sh

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="${DIR}/../../../../.."

pushd ${ROOT}

#--gofast_out=.

#OUT="--gofast_out=."
OUT="--go_out=."
API_PROTOS=$(find "istio.io/istio/galley/pkg/api" -type f -name '*.proto' -d 1 | sort)
protoc -I"istio.io/istio" ${OUT} ${API_PROTOS}

DISTRIB_PROTOS=$(find "istio.io/istio/galley/pkg/api/distrib" -type f -name '*.proto' -d 1 | sort)
protoc -I"istio.io/istio" ${OUT} ${DISTRIB_PROTOS}

popd