#!/usr/bin/env bash

# Copyright Istio Authors
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

# This file is copy form kubernetes.
readonly ISTIO_GO_PACKAGE=istio.io/istio
readonly ISTIO_GOPATH="${ISTIO_OUTPUT}/go"

# Create the GOPATH tree under $ISTIO_OUTPUT
ISTIO::golang::create_gopath_tree() {
  local go_pkg_dir="${ISTIO_GOPATH}/src/${ISTIO_GO_PACKAGE}"
  local go_pkg_basedir
  go_pkg_basedir=$(dirname "${go_pkg_dir}")

  mkdir -p "${go_pkg_basedir}"

  # TODO: This symlink should be relative.
  if [[ ! -e "${go_pkg_dir}" || "$(readlink "${go_pkg_dir}")" != "${ISTIO_ROOT}" ]]; then
    ln -snf "${ISTIO_ROOT}" "${go_pkg_dir}"
  fi
}

# Ensure the go tool exists and is a viable version.
ISTIO::golang::verify_go_version() {
  if [[ -z "$(command -v go)" ]]; then
    ISTIO::log::usage_from_stdin <<EOF
Can't find 'go' in PATH, please fix and retry.
See http://golang.org/doc/install for installation instructions.
EOF
    return 2
  fi

  local go_version
  IFS=" " read -ra go_version <<< "$(GOFLAGS='' go version)"
  local minimum_go_version
  minimum_go_version=go1.16.0
  if [[ "${minimum_go_version}" != $(echo -e "${minimum_go_version}\n${go_version[2]}" | sort -s -t. -k 1,1 -k 2,2n -k 3,3n | head -n1) && "${go_version[2]}" != "devel" ]]; then
    ISTIO::log::usage_from_stdin <<EOF
Detected go version: ${go_version[*]}.
ISTIOrnetes requires ${minimum_go_version} or greater.
Please install ${minimum_go_version} or later.
EOF
    return 2
  fi
}

# ISTIO::golang::setup_env will check that the `go` commands is available in
# ${PATH}. It will also check that the Go version is good enough for the
# ISTIOrnetes build.
#
# Inputs:
#   ISTIO_EXTRA_GOPATH - If set, this is included in created GOPATH
#
# Outputs:
#   env-var GOPATH points to our local output dir
#   env-var GOBIN is unset (we want binaries in a predictable place)
#   env-var GO15VENDOREXPERIMENT=1
#   current directory is within GOPATH
ISTIO::golang::setup_env() {
  ISTIO::golang::verify_go_version

  ISTIO::golang::create_gopath_tree

  export GOPATH="${ISTIO_GOPATH}"
  export GOCACHE="${ISTIO_GOPATH}/cache"

  # Append ISTIO_EXTRA_GOPATH to the GOPATH if it is defined.
  if [[ -n ${ISTIO_EXTRA_GOPATH:-} ]]; then
    GOPATH="${GOPATH}:${ISTIO_EXTRA_GOPATH}"
  fi

  # Make sure our own Go binaries are in PATH.
  export PATH="${ISTIO_GOPATH}/bin:${PATH}"

  # Change directories so that we are within the GOPATH.  Some tools get really
  # upset if this is not true.  We use a whole fake GOPATH here to collect the
  # resultant binaries.  Go will not let us use GOBIN with `go install` and
  # cross-compiling, and `go install -o <file>` only works for a single pkg.
  local subdir
  subdir=$(ISTIO::realpath . | sed "s|${ISTIO_ROOT}||")
  cd "${ISTIO_GOPATH}/src/${ISTIO_GO_PACKAGE}/${subdir}" || return 1

  # Set GOROOT so binaries that parse code can work properly.
  GOROOT=$(go env GOROOT)
  export GOROOT

  # Unset GOBIN in case it already exists in the current session.
  unset GOBIN

  # This seems to matter to some tools
  export GO15VENDOREXPERIMENT=1
}
