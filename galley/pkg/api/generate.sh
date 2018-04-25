#!/bin/sh

set -ex

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="${DIR}/../../../../.."

USE_GOGO=true

pushd ${DIR}
PROTOS="$( find .  -type f -name '*.proto' )"

pushd ${ROOT}

for i in ${PROTOS}; do
    p="istio.io/istio/galley/pkg/api"
    docker run \
        --rm -v ${ROOT}/$p:/src/$p \
        -w /src \
        gcr.io/istio-testing/protoc:2018-03-03 \
        -I${p}\
        --gogo_out=plugins=grpc,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/rpc/status.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/code.proto=github.com/gogo/googleapis/google/rpc,Mgoogle/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc,:. \
        "$p/$i"

    b=$( basename ${i} ".proto" )
    d=$( dirname ${i} )
    f="$p/$d/$b"
    if [ -f "$p/$i" ]; then
        sed -e 's/*google_protobuf.Struct/interface{}/g' \
            -e 's/ValueType_VALUE_TYPE_UNSPECIFIED/VALUE_TYPE_UNSPECIFIED/g' \
            -e 's/istio_policy_v1beta1\.//g' "$f.pb.go" \
            | grep -v "github.com/golang/protobuf/ptypes/struct" | grep -v "import istio_policy_v1beta1" > "${f}_fixed.pb.go"
        rm -f "$f.pb.go"
    fi
done

popd
popd