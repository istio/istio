#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -z "${OUTPUT_DIR}" ]]; then
  OUTPUT_DIR=$ROOT
fi

echo "Generate certificate and private key for Istio CA in ${OUTPUT_DIR}"
unset GOOS && \
unset GOARCH && \

pushd $ROOT

time go build -o ${GOPATH}/bin/generate_cert ./cmd/generate_cert

time ${GOPATH}/bin/generate_cert \
--key-size=2048 \
--out-cert=${OUTPUT_DIR}/istio_ca.crt \
--out-priv=${OUTPUT_DIR}/istio_ca.key \
--organization="k8s.cluster.local" \
--self-signed=true \
--ca=true

echo "Generate certificate and private key for node agent in ${OUTPUT_DIR}"

time ${GOPATH}/bin/generate_cert \
--key-size=2048 \
--out-cert=${OUTPUT_DIR}/node_agent.crt \
--out-priv=${OUTPUT_DIR}/node_agent.key \
--organization="NodeAgent" \
--host="nodeagent.google.com" \
--signer-cert=${OUTPUT_DIR}/istio_ca.crt \
--signer-priv=${OUTPUT_DIR}/istio_ca.key

popd
