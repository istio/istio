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

# This script runs go tests in a package, but each test is run individually. This helps
# isolate tests that are improperly depending on global state modification of other tests

PACKAGE="${1:?test packages}"
PACKAGES="$(go list "${PACKAGE}")"

red='\e[0;31m'
green='\e[0;32m'
yellow='\e[0;33m'
clr='\e[0m'

mkdir -p /tmp/test-results

for p in $PACKAGES; do
  echo "Testing package $p"
  dir=${p#"istio.io/istio/"}
  if go test -o /tmp/test.test -c "${p}" | grep -q 'no test files'; then
    echo -e "    ${yellow}SKIP ${dir}${clr}"
    continue
  fi
  pass=1
  for testname in $(/tmp/test.test -test.list '.*'); do
    (cd "${dir}" || exit; /tmp/test.test -test.run '^'"${testname}"'$' &> "/tmp/test-results/${testname}")
    # shellcheck disable=SC2181
    if [[ $? != 0 ]]; then
      echo -e "    ${red}${testname} failed, see /tmp/test-results/${testname} for full logs$clr"
      pass=0
    fi
  done
  if [[ $pass == 1 ]]; then
    echo -e "    ${green}PASS ${dir}${clr}"
  else
    echo -e "    ${red}FAIL ${dir}${clr}"
  fi
done
