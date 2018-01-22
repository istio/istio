#!/bin/bash

# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# This script fetches envoy API protos at locked versions and generates
# their corresponding gogo flavor of .pb.go files. Additionally, this
# script compensates for https://github.com/golang/dep/issues/1306 due
# to which repos references solely non-go files cannot be added as
# constraints in Gopkg.toml files.
# Args:
#  --clean cleans all affected vendor directories containing generated
#          go files for the Envoy API
#
CLEAN_REPOS=
if [ ! -z $1 ]; then
  if [ "$1" != "--clean" ]; then
    echo -e "\nUsage:\n    bin/build-envoy-api.sh [--clean]\n"
    exit -1
  fi
  CLEAN_REPOS="$1"
fi

# The root path for vendor files
VENDOR_PATH="${GOPATH}/src/istio.io/istio/vendor"

# Preserve old value of IFS that can be used for subsequent read commands
OLD_IFS=$IFS

# get_toml_meta stores the value of the metadata property in TOML_META
# Args:
#   $1 The name of the metadata property
get_toml_meta() {
    local __version_regex="s/^${1}\\s*=\\s*\"\\(.*\\)\".*/\\1/g"
    TOML_META=`cat Gopkg.toml | grep ${1} | sed -e ${__version_regex}`
}

# TODO: remove download-versioned-src() once
# https://github.com/golang/dep/issues/1306 is fixed. Currently
# dep chokes if a constraint only contains non-go files.

# download-versioned-src downloads the dependency, extracts relevant
# folders of proto files excluding those mentioned in the exclusion
# list.
# Args:
#   $1 REPO        The name of the repository, example:
#                  "github.com/envoyproxy".
#   $2 PROJECT     The name of the project under that repository,
#                  example: "data-plane-api".
#   $3 API_VERSION The SHA of the revision that needs to be downloaded
#                  "8e6aaf55f4954f1ef9d3ee2e8f5a50e79cc04f8f".
#   $4 INCLUSIONS  A space separated list of folders containing
#                  protocol buffer definition files having file
#                  extension '.proto'. Use * for wildcards, and must
#                  start with '*/' and end with "*.proto", example:
#                  "*/service/*.proto */ui/*.proto".
#   $5 EXCLUSIONS  A space separated list of directories to exclude
#                  for protobufs that may not be required but are
#                  problematic in terms of dependencies.
download-versioned-src() {
    if [ -z "${1}" ]; then
        echo "Error: No repo specified for download-versioned-src!"
        exit -1
    fi
    REPO="${1}"
    API_PATH="${2}"
    API_VERSION="${3}"
    OPT_INCLUSIONS="${4}"
    OPT_EXCLUSIONS=""
    if [ ! -z "${5}" ]; then
        OPT_EXCLUSIONS="-x ${5}"
    fi
    VENDOR_REPO_PATH="${VENDOR_PATH}/${REPO}"
    if [ ! -z ${CLEAN_REPOS} ]; then
      echo "    ${REPO}/${API_PATH}"
      rm -rf ${VENDOR_REPO_PATH}/*
      return
    fi
    echo "    ${REPO}/${API_PATH} at version: ${API_VERSION}"
    mkdir -p ${VENDOR_REPO_PATH}
    rm -rf ${VENDOR_REPO_PATH}/*
    curl -s -Lo "${VENDOR_REPO_PATH}/${API_VERSION}.zip" https://${REPO}/${API_PATH}/archive/${API_VERSION}.zip
    unzip -qq -o -d ${VENDOR_REPO_PATH}/ ${VENDOR_REPO_PATH}/${API_VERSION}.zip ${OPT_INCLUSIONS} ${OPT_EXCLUSIONS}
    mv ${VENDOR_REPO_PATH}/${API_PATH}-${API_VERSION} ${VENDOR_REPO_PATH}/${API_PATH}
    rm ${VENDOR_REPO_PATH}/${API_VERSION}.zip
}

# List of directories that contain .proto files. This list is necessary to
# accommodate the go compiler plugin restriction where it can only be supplied
# proto files from a single directory. If a repo has protos in nested
# directories, for example:
#   repo.io/some-repo/project/a
#   repo.io/some-repo/project/a/b,
# GO_PACKAGE_DIRS would end up containing 2 entries and each entry which
# would also be the same directories that host go files.
GO_PACKAGE_DIRS=()

# extract_packages builds GO_PACKAGE_DIRS for a repository that may
# contain references to multiple source directories. extract_packages
# finds all the nested directories of each source path and adds each
# of these directories to GO_PACKAGE_DIRS
# Args: (have the same meaning as those of download-versioned-src)
#   $1 REPO
#   $2 PROJECT
#   $3 INCLUSIONS
extract_packages() {
    GO_PACKAGE_PREFIX="vendor/${1}/${2}"
    IFS=' ' read -ra PROTO_PATHS <<< "${3}"
    for PROTO_PATH in "${PROTO_PATHS[@]}"
    do
      # sed regex: 's/\*\(\/\(.*\/\)?\).*/\1/g'
      local __version_regex="s/\\*\\(\\/\\(.*\\/\\)\?\\).*/\\1/g"
      PROTO_PATH_BASE=`echo "${PROTO_PATH}" | sed -e ${__version_regex}`
      PROTO_PATH_SUBDIRS=`find ${GO_PACKAGE_PREFIX}${PROTO_PATH_BASE} -type d`
      for PROTO_PATH_SUBDIR in "${PROTO_PATH_SUBDIRS[@]}"
      do
          GO_PACKAGE_DIRS+=(${PROTO_PATH_SUBDIR})
      done
    done
}

# Gets a list of all APIs from the Gopkg.toml file, downloads the
# appropriate version of the source into vendors/ and builds up
# the GO_PACKAGE_DIRS list.
echo -e "\nFetching Envoy API proto sources from Gopkg.toml"
get_toml_meta "GOGO_PROTO_APIS"
IFS=' ' read -ra APIS <<< "${TOML_META}"
for API in "${APIS[@]}"
do
    get_toml_meta "${API}_REPO"
    REPO="${TOML_META}"
    get_toml_meta "${API}_PROJECT"
    PROJECT="${TOML_META}"
    get_toml_meta "${API}_PROTOS"
    PROTOS="${TOML_META}"
    get_toml_meta "${API}_EXCLUSIONS"
    EXCLUSIONS="${TOML_META}"
    get_toml_meta "${API}_REVISION"
    REVISION="${TOML_META}"
    download-versioned-src "${REPO}" "${PROJECT}" "${REVISION}" "${PROTOS}" "${EXCLUSIONS}"
    if [ ! -z ${CLEAN_REPOS} ]; then
      continue
    fi
    extract_packages "${REPO}" "${PROJECT}" "${PROTOS}"
done

if [ ! -z ${CLEAN_REPOS} ]; then
  echo -e "\nCleaned Envoy API proto gogo generated files!\n"
  exit 0
fi

# The list of root directories to pass to
# the protoc compiler as --proto_path options
imports=(
 "vendor/github.com/envoyproxy/data-plane-api"
 "vendor/github.com/gogo/protobuf"
 "vendor/github.com/gogo/protobuf/protobuf"
 "vendor/github.com/googleapis/googleapis"
 "vendor/github.com/lyft/protoc-gen-validate"
)
for i in "${imports[@]}"
do
  IMPORTS+="--proto_path=$i "
done

# mappings take care of differences between packages in .proto
# files and the corresponding go package paths. This is passed
# to the plugin option of protoc
mappings=(
  "gogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto"
  "google/protobuf/any.proto=github.com/gogo/protobuf/types"
  "google/protobuf/api.proto=github.com/gogo/protobuf/types"
  "google/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
  "google/protobuf/duration.proto=github.com/gogo/protobuf/types"
  "google/protobuf/struct.proto=github.com/gogo/protobuf/types"
  "google/protobuf/timestamp.proto=github.com/gogo/protobuf/types"
  "google/protobuf/type.proto=github.com/gogo/protobuf/types"
  "google/protobuf/wrappers.proto=github.com/gogo/protobuf/types"
  "google/rpc/code.proto=istio.io/gogo-genproto/googleapis/google/rpc"
  "google/rpc/error_details.proto=istio.io/gogo-genproto/googleapis/google/rpc"
  "google/rpc/status.proto=istio.io/gogo-genproto/googleapis/google/rpc"
  "validate/validate.proto=github.com/lyft/protoc-gen-validate/validate"
)
MAPPINGS=""
for i in "${mappings[@]}"
do
  MAPPINGS+="M$i,"
done

# Build the gogo protoc-min-version flavor of protoc
GOGO_VERSION=$(sed -n '/gogo\/protobuf/,/\[\[projects/p' Gopkg.lock | grep version | sed -e 's/^[^\"]*\"//g' -e 's/\"//g')
GOGO_PROTOC="${GOPATH}/bin/protoc-min-version-${GOGO_VERSION}"
if [ ! -f ${GOGO_PROTOC} ]; then
  echo "Building gogo protoc for version ${GOGO_VERSION}"
  GOBIN=${GOPATH}/bin go install vendor/github.com/gogo/protobuf/protoc-min-version/minversion.go
  mv -u ${GOPATH}/bin/minversion ${GOGO_PROTOC}
fi

# Build the gogo protoc-gen-gogofast protoc plugin for generating go files.
GOGOFAST_PROTOC_GEN="${GOPATH}/bin/protoc-gen-gogofast-${GOGO_VERSION}"
if [ ! -f ${GOGOFAST_PROTOC_GEN} ]; then
  echo "Building gogofast protoc gen for version ${GOGO_VERSION}"
  GOBIN=${GOPATH}/bin go install vendor/github.com/gogo/protobuf/protoc-gen-gogofast/main.go
  mv -u ${GOPATH}/bin/main ${GOGOFAST_PROTOC_GEN}
fi

# Switch protoc to use the gogo flavor of protoc
protoc="${GOGO_PROTOC} -version=3.5.0"
PLUGIN="--plugin=${GOGOFAST_PROTOC_GEN} --gogofast-${GOGO_VERSION}_out=plugins=grpc,$MAPPINGS"

# update_go_package determines the output path of the .go file taking into
# consideration inconsistencies in package naming within proto files:
#   - Some protos do not have option go_package specified in the proto at all
#   - Others specify go_package as the current directory ex: "api"
#   - Still others specify a fully qualified package path ex: "github.com/a/b"
# The protocol compiler writes the .go file depending on go_package and without
# the correct output path, the file would be created in directories that are
# incompatible with vendoring.
update_go_package() {
  local __pkg_regex="s/option\\s*go_package\\s*=\\s*\"\\(.*\\)\".*/\\1/g"
  local __go_pkg=`cat ${1} | grep "option go_package" | sed -e ${__pkg_regex}`
  local __re=".*/.*"
  if [[ ! "${__go_pkg}" =~ ${__re} ]]
  then
    local __go_pkg_root=`echo "${1}" | cut -d "/" -f1,2,3,4`
    OUTDIR=":${__go_pkg_root}"
  fi
}

# runprotoc calls protoc with all the protos from a single directory
# Args:
#  $@ GOGO_PROTOS the list of individual .proto paths
runprotoc() {
    OUTDIR=":vendor"
    update_go_package "${1}"
    # echo -e "Running: ${protoc} ${IMPORTS} ${PLUGIN} $@\n"
    err=`${protoc} ${IMPORTS} ${PLUGIN}${OUTDIR} $@`
    if [ ! -z "$err" ]; then
      echo "Error in building Envoy API gogo files:"
      echo "${err}"
      exit -1
    fi
}

# Iterate through GO_PACKAGE_DIRS and calls runprotoc()
# with protos from each directory
echo -e "\nGenerating Envoy API gogo files"
for PACKAGE_DIR in "${GO_PACKAGE_DIRS[@]}"
do
    echo "    proto-source: ${PACKAGE_DIR}"
    GOGO_PROTOS=`find ${PACKAGE_DIR} -maxdepth 1 -name "*.proto"`
    runprotoc ${GOGO_PROTOS}
done
echo -e "\nDone building Envoy API proto gogo files!\n"
