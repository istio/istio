#!/bin/bash

# Copyright 2017 Google Inc. All rights reserved.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script extracts the CA certs from the typical debian ca-certificates debian package.
# It would be nicer to do this in Python or Go, but neither of these languages have packages
# that can extract .xz files in their stdlibs.

# Taken, verbatim, from:
# https://github.com/GoogleCloudPlatform/distroless/blob/master/cacerts/extract.sh
# It was copied, as the distroless skylark rules/build for ca-certs have restricted
# visibility.


DEB=$1
CERTS_PATH=$2

ar -x $DEB data.tar.xz
tar -xf data.tar.xz ./usr/share/ca-certificates
tar -xf data.tar.xz ./usr/share/doc/ca-certificates/copyright

# Concat all the certs.
CERT_FILE=./etc/ssl/certs/ca-certificates.crt
mkdir -p $(dirname $CERT_FILE)

CERTS=$(find usr/share/ca-certificates -type f | sort)
for cert in $CERTS; do
  cat $cert >> $CERT_FILE
done

tar -cf $2 etc/ssl/certs/ca-certificates.crt usr/share/doc/ca-certificates/copyright

rm data.tar.xz
rm -rf usr/share/ca-certificates
