#!/usr/bin/env bash

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
