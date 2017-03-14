#!/bin/bash
set -x
# This file should be replaced by bazel
# 
# This file works around the issue that go jsonpb does not support
# ptype.Struct parsing yet
# https://github.com/istio/mixer/issues/134
# It does so by replacing Struct with interface{}.
# From this point on the protos will only have a valid external representation as json.
WD=$(dirname $0)
WD=$(cd $WD; pwd)
PKG=$(dirname $WD)

BUILD_DIR=$(dirname $(dirname $(readlink  ${WD}/../../../bazel-mixer)))
ISTIO_API=${BUILD_DIR}/external/com_github_istio_api

cd ${ISTIO_API}
CKSUM=$(cksum mixer/v1/config/cfg.proto)

# protoc keeps the directory structure leading up to the proto file, so it creates
# the dir ${WD}/mixer/v1/config/. We don't want that, so we'll move the pb.go file
# out and remove the dir.
protoc mixer/v1/config/cfg.proto --go_out=${WD}
mv ${WD}/mixer/v1/config/cfg.pb.go ${WD}/cfg.pb.go
rm -rd ${WD}/mixer

cd ${WD}
TMPF="_cfg.pb.go"
PB="cfg.pb.go"

echo "// POST PROCESSED USING by build_cfg.sh"  > ${TMPF}
echo "// ${CKSUM}"  >> ${TMPF}
cat ${PB} >> ${TMPF}
mv ${TMPF} ${PB}

sed -i 's/*google_protobuf.Struct/interface{}/g' ${PB}
sed -i 's/mixer\/v1\/config\/descriptor/istio.io\/api\/mixer\/v1\/config\/descriptor/g' ${PB}

goimports -w ${PB}
