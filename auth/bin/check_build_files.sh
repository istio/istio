#!/bin/bash

set -ex

diff_size=$(bazel run //:gazelle -- -mode diff | wc -c)

if [ "$diff_size" -ne "0" ]; then
  echo "Some BUILD files are incorrect"
  echo "Please run 'bazel run //:gazelle' and commit the changes"

  exit -1
fi
