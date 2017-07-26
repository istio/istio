#!/bin/bash

set -ex

diff_size=$(gazelle -go_prefix istio.io/auth -mode diff | wc -c)

if [ "$diff_size" -ne "0" ]; then
  echo "Some BUILD files are incorrect"
  echo "Please run 'gazelle -go_prefix istio.io/auth -mode fix' and commit the changes"

  exit -1
fi
