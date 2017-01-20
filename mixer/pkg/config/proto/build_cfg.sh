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


BLDDIR=$(dirname $(dirname $(readlink  ${WD}/../../../bazel-mixer)))

ISTIO_API=${BLDDIR}/external/com_github_istio_api

cd ${ISTIO_API}/mixer/v1/config

CKSUM=$(cksum cfg.proto)

protoc cfg.proto --go_out=${WD} 
cd ${WD}
TMPF="_cfg.pb.go"
PB="cfg.pb.go"

echo "// POST PROCESSED USING by build_cfg.sh"  > ${TMPF}
echo "// ${CKSUM}"  >> ${TMPF}
cat ${PB} >> ${TMPF}
mv ${TMPF} ${PB}

sed -i "" 's/*google_protobuf.Struct/interface{}/g' ${PB}

goimports -w ${PB}
