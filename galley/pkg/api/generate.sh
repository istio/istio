#!/bin/sh

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="${DIR}/../../../../.."

USE_GOGO=true

pushd ${ROOT}
if [ ${USE_GOGO} == true ]; then

    docker run \
        --rm -v /Users/ozben/go/src/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        serviceconfig.proto

    docker run \
        --rm -v /Users/ozben/go/src/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        distrib/mixerconfig.proto
else
    OUT="--go_out=."

    API_PROTOS=$(find "istio.io/istio/galley/pkg/api" -type f -name '*.proto' -d 1 | sort)
    protoc -I"istio.io/istio" ${OUT} ${API_PROTOS}

    DISTRIB_PROTOS=$(find "istio.io/istio/galley/pkg/api/distrib" -type f -name '*.proto' -d 1 | sort)
    protoc -I"istio.io/istio" ${OUT} ${DISTRIB_PROTOS}
fi

    if [ -f "istio.io/istio/galley/pkg/api/serviceconfig.pb.go" ]; then
        sed -e 's/*google_protobuf.Struct/interface{}/g' \
            -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
            -e 's/istio_policy_v1beta1\.//g' istio.io/istio/galley/pkg/api/serviceconfig.pb.go \
            | grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >istio.io/istio/galley/pkg/api/serviceconfig_fixed.pb.go
        rm -f istio.io/istio/galley/pkg/api/serviceconfig.pb.go
    fi

    if [ -f "istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go" ]; then
        sed -e 's/*google_protobuf.Struct/interface{}/g' \
            -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
            -e 's/istio_policy_v1beta1\.//g' istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go \
            | grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >istio.io/istio/galley/pkg/api/distrib/mixerconfig_fixed.pb.go
        rm -f istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go
    fi

popd