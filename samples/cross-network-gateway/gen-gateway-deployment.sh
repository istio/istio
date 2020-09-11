#!/usr/bin/env bash

CLUSTER_NAME=${CLUSTER_NAME:-"SPECIFY A CLUSTER NAME"}
NETWORK_NAME=${NETWORK_NAME:-"SPECIFY A NETWORK NAME"}

cat << EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: empty
  values:
    global:
      multiCluster:
        clusterName: ${CLUSTER_NAME}
  components:
    ingressGateways:
      - name: istio-east-west-gateway
        label:
          istio: east-west-gateway
          app: istio-east-west-gateway
        enabled: true
        k8s:
          env:
            # traffic through this gateway should be routed inside the network
            - name: ISTIO_META_REQUESTED_NETWORK_VIEW
              value: ${NETWORK_NAME}
EOF