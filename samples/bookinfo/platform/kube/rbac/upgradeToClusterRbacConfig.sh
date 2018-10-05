#!/bin/bash
set -e
set -u

# This script converts RbacConfig to ClusterRbacConfig and deletes the RbacConfig after ClusterRbacConfig
# is successfully applied. The RbacConfig is deprecated by ClusterRbacConfig.

RBAC_CONFIGS=$(kubectl get RbacConfig --all-namespaces --no-headers --ignore-not-found)
if [ "${RBAC_CONFIGS}" == "" ]
then
  echo "RbacConfig not found"
  exit 0
fi

RBAC_CONFIG_COUNT=$(echo "${RBAC_CONFIGS}" | wc -l)
if [ "${RBAC_CONFIG_COUNT}" -ne 1 ]
then
  echo "${RBAC_CONFIGS}"
  echo "found ${RBAC_CONFIG_COUNT} RbacConfigs, expecting only 1"
  exit 1
fi

NS=$(echo "${RBAC_CONFIGS}" | cut -f 1 -d ' ')
echo "converting RbacConfig in namespace $NS to ClusterRbacConfig"

SPEC=$(kubectl get RbacConfig default -n "${NS}" -o yaml | sed -n -e '/spec:/,$p')

cat <<EOF | kubectl apply -n "${NS}" -f -
apiVersion: "rbac.istio.io/v1alpha1"
kind: ClusterRbacConfig
metadata:
  name: default
${SPEC}
EOF

# shellcheck disable=SC2181
if [ $? -ne 0 ]
then
  echo "failed to apply ClusterRbacConfig"
  exit 1
fi

echo "waiting for 15 seconds to delete RbacConfig"
sleep 15
kubectl delete RbacConfig --all -n "${NS}"
