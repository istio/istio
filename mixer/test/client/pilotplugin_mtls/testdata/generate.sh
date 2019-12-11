#!/bin/sh

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

openssl genrsa -out root.key 2048
openssl req -x509 -new -nodes -key root.key -sha256 -days 1825 -out root.cert

# generate mTLS cert for client as follows:
go run security/tools/generate_cert/main.go -host="spiffe://cluster.local/ns/default/sa/client" -signer-priv=mixer/test/client/pilotplugin_mtls/testdata/root.key -signer-cert=mixer/test/client/pilotplugin_mtls/testdata/root.cert --mode=signer
