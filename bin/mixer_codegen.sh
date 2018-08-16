#!/bin/bash

die () {
  echo "ERROR: $*. Aborting." >&2
  exit 1
}

WD="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT="$(dirname "$WD")"

if [ ! -e "$ROOT/Gopkg.lock" ]; then
  echo "Please run 'dep ensure' first"
  exit 1
fi

set -e

outdir=$ROOT
file=$ROOT
protoc="$ROOT/bin/protoc.sh"

optimport=$ROOT
template=$ROOT

optproto=false
optadapter=false
opttemplate=false
gendoc=true
# extra flags are arguments that are passed to the underlying tool verbatim
# Its value depend on the context of the main generation flag.
# * for parent flag `-a`, the `-x` flag can provide additional options required by tool mixer/tool/mixgen adapter --help
extraflags=""

while getopts ':f:o:p:i:t:a:d:x:' flag; do
  case "${flag}" in
    f) $opttemplate && $optadapter && die "Cannot use proto file option (-f) with template file option (-t) or adapter option (-a)"
       optproto=true
       file+="/${OPTARG}"
       ;;
    a) $opttemplate && $optproto && die "Cannot use proto adapter option (-a) with template file option (-t) or file option (-f)"
       optadapter=true
       file+="/${OPTARG}"
       ;;
    o) outdir="${OPTARG}" ;;
    p) protoc="${OPTARG}" ;;
    x) extraflags="${OPTARG}" ;;
    i) optimport+=/"${OPTARG}" ;;
    t) $optproto && $optadapter && die "Cannot use template file option (-t) with proto file option (-f) or adapter option (-a)"
       opttemplate=true
       template+="/${OPTARG}"
       ;;
    d) gendoc="${OPTARG}" ;;
    *) die "Unexpected option ${flag}" ;;
  esac
done

# echo "outdir: ${outdir}"

# Ensure expected GOPATH setup
if [ "$ROOT" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

imports=(
 "${ROOT}"
 "${ROOT}/vendor/istio.io/api"
 "${ROOT}/vendor/github.com/gogo/protobuf"
 "${ROOT}/vendor/github.com/gogo/googleapis"
 "${ROOT}/vendor/github.com/gogo/protobuf/protobuf"
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
  "google/rpc/status.proto=github.com/gogo/googleapis/google/rpc"
  "google/rpc/code.proto=github.com/gogo/googleapis/google/rpc"
  "google/rpc/error_details.proto=github.com/gogo/googleapis/google/rpc"
)

MAPPINGS=""

for i in "${mappings[@]}"
do
  MAPPINGS+="M$i,"
done

PLUGIN="--gogoslick_out=plugins=grpc,$MAPPINGS:"
PLUGIN+=$outdir

GENDOCS_PLUGIN="--docs_out=warnings=true,mode=html_fragment_with_front_matter:"
GENDOCS_PLUGIN_FILE=$GENDOCS_PLUGIN$(dirname "${file}")
GENDOCS_PLUGIN_TEMPLATE=$GENDOCS_PLUGIN$(dirname "${template}")

# handle template code generation
if [ "$opttemplate" = true ]; then

  template_mappings=(
    "google/protobuf/any.proto:github.com/gogo/protobuf/types"
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

  TMPL_PLUGIN="--gogoslick_out=plugins=grpc,$TMPL_PROTOC_MAPPING:"
  TMPL_PLUGIN+=$outdir

  descriptor_set="_proto.descriptor_set"
  handler_gen_go="_handler.gen.go"
  handler_service="_handler_service.proto"
  pb_go=".pb.go"

  templateDS=${template/.proto/$descriptor_set}
  templateHG=${template/.proto/$handler_gen_go}
  templateHSP=${template/.proto/$handler_service}
  templatePG=${template/.proto/$pb_go}
  # generate the descriptor set for the intermediate artifacts
  DESCRIPTOR="--include_imports --include_source_info --descriptor_set_out=$templateDS"
  if [ "$gendoc" = true ]; then
    err=$($protoc "$DESCRIPTOR" "$IMPORTS" "$PLUGIN" "$GENDOCS_PLUGIN_TEMPLATE" "$template")
  else
    err=$($protoc "$DESCRIPTOR" "$IMPORTS" "$PLUGIN" "$template")
  fi
  if [ ! -z "$err" ]; then
    die "template generation failure: $err";
  fi

  IFS=" " read -r -a TMPL_GEN_MAP_ARRAY <<< "$TMPL_GEN_MAP"
  go run "$GOPATH/src/istio.io/istio/mixer/tools/mixgen/main.go" api -t "$templateDS" --go_out "$templateHG" --proto_out "$templateHSP" "${TMPL_GEN_MAP_ARRAY[@]}"

  err=$($protoc "$IMPORTS" "$TMPL_PLUGIN" "$templateHSP")
  if [ ! -z "$err" ]; then
    die "template generation failure: $err";
  fi

  templateSDS=${template/.proto/_handler_service.descriptor_set}
  SDESCRIPTOR="--include_imports --include_source_info --descriptor_set_out=$templateSDS"
  err=$($protoc "$SDESCRIPTOR" "$IMPORTS" "$PLUGIN" "$templateHSP")
  if [ ! -z "$err" ]; then
    die "template generation failure: $err";
  fi

  templateYaml=${template/.proto/.yaml}
  go run "$GOPATH/src/istio.io/istio/mixer/tools/mixgen/main.go" template -d "$templateSDS" -o "$templateYaml" -n "$(basename "$(dirname "${template}")")"

  rm "$templatePG"

  exit 0
fi

# handle adapter code generation
if [ "$optadapter" = true ]; then
  if [ "$gendoc" = true ]; then
    err=$($protoc "$IMPORTS" "$PLUGIN" "$GENDOCS_PLUGIN_FILE" "$file")
  else
    err=$($protoc "$IMPORTS" "$PLUGIN" "$file")
  fi
  if [ ! -z "$err" ]; then
    die "generation failure: $err";
  fi

  adapteCfdDS=${file}_descriptor
  err=$($protoc "$IMPORTS" "$PLUGIN" --include_imports --include_source_info --descriptor_set_out="${adapteCfdDS}" "$file")
  if [ ! -z "$err" ]; then
  die "config generation failure: $err";
  fi

  IFS=" " read -r -a extraflags_array <<< "$extraflags"
  go run "$GOPATH/src/istio.io/istio/mixer/tools/mixgen/main.go" adapter -c "$adapteCfdDS" -o "$(dirname "${file}")" "${extraflags_array[@]}"

  exit 0
fi

# handle simple protoc-based generation
if [ "$gendoc" = true ]; then
  err=$($protoc "$IMPORTS" "$PLUGIN" "$GENDOCS_PLUGIN_FILE" "$file")
else
  err=$($protoc "$IMPORTS" "$PLUGIN" "$file")
fi
if [ ! -z "$err" ]; then
  die "generation failure: $err";
fi

err=$($protoc "$IMPORTS" "$PLUGIN" --include_imports --include_source_info --descriptor_set_out="${file}_descriptor" "$file")
if [ ! -z "$err" ]; then
die "config generation failure: $err";
fi
