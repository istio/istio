#!/bin/sh

set -ex

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="${DIR}/../../../../.."

USE_GOGO=true

pushd ${ROOT}

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        networking/v1alpha3/destination_rule.proto

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        networking/v1alpha3/virtual_service.proto

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        networking/v1alpha3/gateway.proto

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        networking/v1alpha3/external_service.proto

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        distrib/mixerconfig.proto

    docker run \
        --rm -v ${ROOT}/istio.io/istio/galley/pkg/api:/src/istio.io/istio/galley/pkg/api \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -Iistio.io/istio/galley/pkg/api\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        service/dev/service_config.proto

    if [ -f "istio.io/istio/galley/pkg/api/service/dev/service_config.pb.go" ]; then
        sed -e 's/*google_protobuf.Struct/interface{}/g' \
            -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
            -e 's/istio_policy_v1beta1\.//g' istio.io/istio/galley/pkg/api/service/dev/service_config.pb.go \
            | grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >istio.io/istio/galley/pkg/api/service/dev/service_config_fixed.pb.go
        rm -f istio.io/istio/galley/pkg/api/service/dev/serviceconfig.pb.go
    fi

    if [ -f "istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go" ]; then
        sed -e 's/*google_protobuf.Struct/interface{}/g' \
            -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
            -e 's/istio_policy_v1beta1\.//g' istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go \
            | grep -v "google_protobuf" | grep -v "import istio_policy_v1beta1" >istio.io/istio/galley/pkg/api/distrib/mixerconfig_fixed.pb.go
        rm -f istio.io/istio/galley/pkg/api/distrib/mixerconfig.pb.go
    fi

popd