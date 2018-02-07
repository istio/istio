#!/usr/bin/env bash

die () {
  echo "ERROR: $*. Aborting." >&2
  exit 1
}

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

if [ ! -e $ROOT/Gopkg.lock ]; then
  echo "Please run 'dep ensure' first"
  exit 1
fi

GOGO_VERSION=$(sed -n '/gogo\/protobuf/,/\[\[projects/p' $ROOT/Gopkg.lock | grep version | sed -e 's/^[^\"]*\"//g' -e 's/\"//g')
GENDOCS_VERSION=$(sed -n '/protoc-gen-docs/,/\[\[projects/p' $ROOT/Gopkg.lock | grep revision | sed -e 's/^[^\"]*\"//g' -e 's/\"//g')

set -e

outdir=$ROOT
file=$ROOT
protoc="$ROOT/bin/protoc-min-version-$GOGO_VERSION -version=3.5.0"

# BUGBUG: we override the use of protoc-min-version here, since using
#         that tool prevents warnings from protoc-gen-docs from being
#         displayed. If protoc-min-version gets fixed to allow this
#         data though, then remove this override
protoc="protoc"

optimport=$ROOT
template=$ROOT

optproto=false
opttemplate=false

while getopts ':f:o:p:i:t:' flag; do
  case "${flag}" in
    f) $opttemplate && die "Cannot use proto file option (-f) with template file option (-t)"
       optproto=true
       file+="/${OPTARG}" 
       ;;
    o) outdir="${OPTARG}" ;;
    p) protoc="${OPTARG}" ;;
    i) optimport+=/"${OPTARG}" ;;
    t) $optproto && die "Cannot use template file option (-t) with proto file option (-f)"
       opttemplate=true
       template+="/${OPTARG}"
       ;;
    *) die "Unexpected option ${flag}" ;;
  esac
done

# echo "outdir: ${outdir}"

# Ensure expected GOPATH setup
if [ $ROOT != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

GOGOPROTO_PATH=vendor/github.com/gogo/protobuf
GOGOSLICK=protoc-gen-gogoslick
GOGOSLICK_PATH=$ROOT/$GOGOPROTO_PATH/$GOGOSLICK
GENDOCS=protoc-gen-docs
GENDOCS_PATH=vendor/github.com/istio/tools/$GENDOCS

if [ ! -e $ROOT/bin/$GOGOSLICK-$GOGO_VERSION ]; then
echo "Building protoc-gen-gogoslick..."
pushd $ROOT
go build --pkgdir $GOGOSLICK_PATH -o $ROOT/bin/$GOGOSLICK-$GOGO_VERSION ./$GOGOPROTO_PATH/$GOGOSLICK
popd
echo "Done."
fi

if [ ! -e $ROOT/bin/$GENDOCS-$GENDOCS_VERSION ]; then
echo "Building protoc-gen-docs..."
pushd $ROOT/$GENDOCS_PATH
go build --pkgdir $GENDOCS_PATH -o $ROOT/bin/$GENDOCS-$GENDOCS_VERSION
popd
echo "Done."
fi

PROTOC_MIN_VERSION=protoc-min-version
MIN_VERSION_PATH=$ROOT/$GOGOPROTO_PATH/$PROTOC_MIN_VERSION

if [ ! -e $ROOT/bin/$PROTOC_MIN_VERSION-$GOGO_VERSION ]; then
echo "Building protoc-min-version..."
pushd $ROOT
go build --pkgdir $MIN_VERSION_PATH -o $ROOT/bin/$PROTOC_MIN_VERSION-$GOGO_VERSION ./$GOGOPROTO_PATH/$PROTOC_MIN_VERSION
popd
echo "Done."
fi

GOOGLEAPIS_SHA=c8c975543a134177cc41b64cbbf10b88fe66aa1d
GOOGLEAPIS_URL=https://raw.githubusercontent.com/googleapis/googleapis/${GOOGLEAPIS_SHA}

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

PLUGIN="--plugin=$ROOT/bin/protoc-gen-gogoslick-$GOGO_VERSION --gogoslick-${GOGO_VERSION}_out=plugins=grpc,$MAPPINGS:"
PLUGIN+=$outdir

GENDOCS_PLUGIN="--plugin=$ROOT/bin/$GENDOCS-$GENDOCS_VERSION --docs-${GENDOCS_VERSION}_out=warnings=true,mode=jekyll_html:"
GENDOCS_PLUGIN_FILE=$GENDOCS_PLUGIN$(dirname "${file}")
GENDOCS_PLUGIN_TEMPLATE=$GENDOCS_PLUGIN$(dirname "${template}")

# handle template code generation
if [ "$opttemplate" = true ]; then

  template_mappings=(
    "mixer/v1/config/descriptor/value_type.proto:istio.io/api/mixer/v1/config/descriptor"
    "mixer/v1/template/extensions.proto:istio.io/api/mixer/v1/template"
    "mixer/v1/template/standard_types.proto:istio.io/api/mixer/v1/template"
    "gogoproto/gogo.proto:github.com/gogo/protobuf/gogoproto"
    "google/protobuf/duration.proto:github.com/gogo/protobuf/types"
  )

  TMPL_GEN_MAP=""
  TMPL_PROTOC_MAPPING=""

  for i in "${template_mappings[@]}"
  do
    TMPL_GEN_MAP+="-m $i "
    TMPL_PROTOC_MAPPING+="M${i/:/=},"
  done

  TMPL_PLUGIN="--plugin=$ROOT/bin/protoc-gen-gogoslick-$GOGO_VERSION --gogoslick-${GOGO_VERSION}_out=$TMPL_PROTOC_MAPPING:"
  TMPL_PLUGIN+=$outdir

  descriptor_set="_proto.descriptor_set"
  handler_gen_go="_handler.gen.go"
  instance_proto="_instance.proto"
  pb_go=".pb.go"

  templateDS=${template/.proto/$descriptor_set}
  templateHG=${template/.proto/$handler_gen_go}
  templateIP=${template/.proto/$instance_proto}
  templatePG=${template/.proto/$pb_go}

  # generate the descriptor set for the intermediate artifacts
  DESCRIPTOR="--include_imports --include_source_info --descriptor_set_out=$templateDS"
  err=`$protoc $DESCRIPTOR $IMPORTS $PLUGIN $GENDOCS_PLUGIN_TEMPLATE $template`
  if [ ! -z "$err" ]; then
    die "template generation failure: $err"; 
  fi
  
  go run $GOPATH/src/istio.io/istio/mixer/tools/codegen/cmd/mixgenproc/main.go $templateDS -o $templateHG -t $templateIP $TMPL_GEN_MAP  

  err=`$protoc $IMPORTS $TMPL_PLUGIN $templateIP`
  if [ ! -z "$err" ]; then 
    die "template generation failure: $err"; 
  fi

  rm $templateIP
  rm $templatePG

  exit 0
fi

# handle simple protoc-based generation
err=`$protoc $IMPORTS $PLUGIN $GENDOCS_PLUGIN_FILE $file`
if [ ! -z "$err" ]; then 
  die "generation failure: $err"; 
fi
