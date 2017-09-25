#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

# Use this file for Cloud Builder specific settings.

echo 'Setting bazel.rc'
cp tools/bazel.rc.cloudbuilder "${HOME}/.bazelrc"

script/release.sh ${@}
