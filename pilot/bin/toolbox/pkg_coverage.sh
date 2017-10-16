#!/bin/bash
set -ex

detected_OS=`uname -s 2>/dev/null || echo not`
BUILD_FLAGS=""
if [ "${detected_OS}" == "Darwin" ]; then # Mac OS X
    BUILD_FLAGS="--cpu=k8"
fi

bazel build ${BUILD_FLAGS} @com_github_istio_test_infra//toolbox/pkg_check \
  || { echo 'Failed to build pkg_check'; exit 0; }
bazel-bin/external/com_github_istio_test_infra/toolbox/pkg_check/pkg_check
