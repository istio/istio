#!/bin/bash
set -e
set -u

# This script is provided to help converting RbacConfig to ClusterRbacConfig automatically. The RbacConfig
# will be deleted after the corresponding ClusterRbacConfig is successfully applied.
# The RbacConfig is deprecated by ClusterRbacConfig due to an implementation bug that could cause the
# RbacConfig to be namespace scoped in some cases. The ClusterRbacConfig has exactly same specification
# as the RbacConfig, but with correctly implemented cluster scope.

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
  echo "found ${RBAC_CONFIG_COUNT} RbacConfigs, expecting only 1. Please delete extra RbacConfigs and execute again."
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
