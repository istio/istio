#!/bin/bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make updatecommon".

# Copyright 2019 Istio Authors
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

set -e

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}")" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

CD_TMPFILE=$(mktemp /tmp/check_dockerfile.XXXXXX)
HL_TMPFILE=$(mktemp /tmp/hadolint.XXXXXX)

# shellcheck disable=SC2044
for df in $(find "${ROOTDIR}" -path "${ROOTDIR}/vendor" -prune -o -name 'Dockerfile*'); do
  docker run --rm -i hadolint/hadolint:v1.17.1 < "$df" > "${HL_TMPFILE}"
  if [ "" != "$(cat "${HL_TMPFILE}")" ]
  then
    {
      echo "$df:"
      cut -d":" -f2- < "${HL_TMPFILE}"
      echo
    } >> "${CD_TMPFILE}"
  fi
done

rm -f "${HL_TMPFILE}"
if [ "" != "$(cat "${CD_TMPFILE}")" ]; then
  cat "${CD_TMPFILE}"
  rm -f "${CD_TMPFILE}"
  exit 1
fi
rm -f "${CD_TMPFILE}"
