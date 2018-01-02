#!/usr/bin/env bash

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

set -e

outdir=$ROOT
file=$ROOT
protoc="protoc-min-version -version=3.5.0"
optimport=$ROOT

while getopts 'f:o:p:i:' flag; do
  case "${flag}" in
    f) file+="/${OPTARG}" ;;
    o) outdir="${OPTARG}" ;;
    p) protoc="${OPTARG}" ;;
    i) optimport+=/"${OPTARG}" ;;
    *) error "Unexpected option ${flag}" ;;
  esac
done

# echo "outdir: ${outdir}"

# Ensure expected GOPATH setup
if [ $ROOT != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

if [ ! -e $GOPATH/bin/protoc-gen-gogoslick ]; then
echo "Installing protoc-gen-gogoslick..."
pushd $ROOT
go install "./vendor/github.com/gogo/protobuf/protoc-gen-gogoslick"
popd
echo "Done."
fi

if [ ! -e $GOPATH/bin/protoc-min-version ]; then
echo "Installing protoc-min-version..."
pushd $ROOT
go install "./vendor/github.com/gogo/protobuf/protoc-min-version"
popd
echo "Done."
fi

SHA=c8c975543a134177cc41b64cbbf10b88fe66aa1d
GOOGLEAPIS_URL=https://raw.githubusercontent.com/googleapis/googleapis/${SHA}

if [ ! -e ${ROOT}/vendor/github.com/googleapis/googleapis ]; then
echo "Pull down source protos from googleapis..."

mkdir -p ${ROOT}/vendor/github.com/googleapis/googleapis

# all the google_rpc protos
mkdir -p ${ROOT}/vendor/github.com/googleapis/googleapis/google/rpc
curl -sS ${GOOGLEAPIS_URL}/google/rpc/status.proto > ${ROOT}/vendor/github.com/googleapis/googleapis/google/rpc/status.proto
curl -sS ${GOOGLEAPIS_URL}/google/rpc/code.proto > ${ROOT}/vendor/github.com/googleapis/googleapis/google/rpc/code.proto
curl -sS ${GOOGLEAPIS_URL}/google/rpc/error_details.proto > ${ROOT}/vendor/github.com/googleapis/googleapis/google/rpc/error_details.proto
fi

imports=(
 "${ROOT}"
 "${ROOT}/vendor/istio.io/api"
 "${ROOT}/vendor/github.com/gogo/protobuf"
 "${ROOT}/vendor/github.com/gogo/protobuf/protobuf"
 "${ROOT}/vendor/github.com/googleapis/googleapis"
)

IMPORTS=""

for i in "${imports[@]}"
do
  IMPORTS+="--proto_path=$i "
done

IMPORTS+="--proto_path=$optimport "

mappings=(
  "gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto"
  "google/protobuf/any.proto=github.com/gogo/protobuf/types"
  "google/protobuf/duration.proto=github.com/gogo/protobuf/types"
  "google/rpc/status.proto=istio.io/gogo-genproto/googleapis/google/rpc"
  "google/rpc/code.proto=istio.io/gogo-genproto/googleapis/google/rpc"
  "google/rpc/error_details.proto=istio.io/gogo-genproto/googleapis/google/rpc"
)

MAPPINGS=""

for i in "${mappings[@]}"
do
  MAPPINGS+="M$i,"
done


PLUGIN="--gogoslick_out=$MAPPINGS:"
PLUGIN+=$outdir

# echo $protoc $IMPORTS $PLUGIN $file
$protoc $IMPORTS $PLUGIN $file
