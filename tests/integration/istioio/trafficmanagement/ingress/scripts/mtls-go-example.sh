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

folderName="httpbin.example.com"
if [[ $1 != "" ]]; then
        folderName=$1
fi
cn="httpbin.example.com"
if [[ $2 != "" ]]; then
        cn=$2
fi

echo "Generate client and server certificates and keys"

if [[ ! -d "mtls-go-example" ]]; then
    git clone https://github.com/nicholasjackson/mtls-go-example
    # Bypass prompt to enter yes when signing certificates.
    sed -i 's/openssl ca /openssl ca -batch /g' mtls-go-example/generate.sh
fi

pushd mtls-go-example || exit

./generate.sh "${cn}" password

mkdir ../"${folderName}" && mv 1_root 2_intermediate 3_application 4_client ../"${folderName}"

popd || exit