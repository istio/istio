#!/bin/bash

kubectl label namespace default istio-injection=enabled || true

cat <<EOF | kubectl apply -n default -f -
apiVersion: authentication.istio.io/v1alpha1
kind: Policy
metadata:
  name: "default"
spec:
  peers:
  - mtls: {}
---
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "default"
spec:
  host: "*.default.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF
