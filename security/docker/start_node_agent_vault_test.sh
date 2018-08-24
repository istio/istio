#!/bin/bash

echo "Starting NodeAgent..."
# Run node-agent
/usr/local/bin/node_agent \
  --env vault \
  --k8s-service-account /usr/local/bin/example-workload-pod-sa.jwt \
  --ca-address istio-standalone-citadel.default.svc.cluster.local:8060 \
  --cert-chain /usr/local/bin/node_agent.crt \
  --key /usr/local/bin/node_agent.key \
  --workload-cert-ttl 90s \
  --root-cert /usr/local/bin/istio_ca.crt
#  --root-cert /usr/local/bin/istio_ca.crt >/var/log/node-agent.log 2>&1 &
