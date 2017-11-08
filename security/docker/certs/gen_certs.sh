#!/bin/bash

echo "Generate certificate and private key for Istio CA"
bazel run $BAZEL_ARGS //security/cmd/generate_cert -- \
-out-cert=${CERTS_OUTPUT_DIR}/istio_ca.crt \
-out-priv=${CERTS_OUTPUT_DIR}/istio_ca.key \
-organization="k8s.cluster.local" \
-self-signed=true \
-ca=true

echo "Generate certificate and private key for node agent"
bazel run $BAZEL_ARGS //security/cmd/generate_cert -- \
-out-cert=${CERTS_OUTPUT_DIR}/node_agent.crt \
-out-priv=${CERTS_OUTPUT_DIR}/node_agent.key \
-organization="NodeAgent" \
-host="nodeagent.google.com" \
-signer-cert=${CERTS_OUTPUT_DIR}/istio_ca.crt \
-signer-priv=${CERTS_OUTPUT_DIR}/istio_ca.key
