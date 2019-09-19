#!/bin/bash

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

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}")" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"|| exit

# shellcheck disable=SC2044
for df in $(find "${ROOTDIR}" -path "${ROOTDIR}/vendor" -prune -o -name 'Dockerfile*'); do
  if [ "" != "$(grep -E -w "apt-get" "${df}")" ]
  then
    BASE="$(grep -E "FROM" "${df}" | cut -d ' ' -f2)"
    URL_CHECK=""
    PATTERN="<h1>Package: "
    EXTRACTION='s/.*(\([^)].*\)).*$/\1/'
    case $BASE in
      "ubuntu:bionic")
        URL_CHECK="https://packages.ubuntu.com/bionic/PACKAGE"
        ;;
      "ubuntu:xenial")
        URL_CHECK="https://packages.ubuntu.com/xenial/PACKAGE"
        ;;
      "debian:9-slim")
        URL_CHECK="https://packages.debian.org/stretch/PACKAGE"
        ;;
      *)
        echo "this base dockerfile image is not supported for version recommendation!"
        ;;
    esac
    # First sed makes a start/stop pattern, grep check lines without apt install / # clean
    # last seds trim spaces and remaining ampersands
    sed -n -e '/apt-get install/,/&&/ p' < "${df}" | grep -E -v "apt-get " | sed -e 's/ \\$//' | sed -e 's/ &&//' | while read -r PACKAGE_VERSION; do
      PACKAGE="$( echo "$PACKAGE_VERSION" | cut -d '=' -f1 )"
      VERSION="$( echo "$PACKAGE_VERSION" | cut -d '=' -f2 )"
      URL="${URL_CHECK//PACKAGE/${PACKAGE}}"
      # The last cut command makes sure there is no comment in the version
      # e.g. 7.58.0-2ubuntu3.8 and others
      NEW_VERSION="$( curl -sfL "$URL" | grep -E "${PATTERN}" | sed -e "${EXTRACTION}" | cut -d " " -f1 )"
      if [ "${NEW_VERSION}" != "${VERSION}" ] ; then
        echo "Update ${df} ${PACKAGE} version from ${VERSION} to ${NEW_VERSION}"
      fi
    done
  fi
done
