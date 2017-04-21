#!/bin/bash

# Will update the kubernetes/istio-rbac.yaml file, based on kubernetes/istio-install configs.
# Original file content will be lost.

SP=$( cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P )

set -ex

# TODO(costin): run part of the presubmit
echo "# GENERATED FILE, DO NOT EDIT" > $SP/istio-rbac.yaml
echo "# TO UPDATE, modify files in istio-install and run rbac.sh" >> $SP/istio-rbac.yaml

cat $SP/istio-install/istio-ca.yaml >> $SP/istio-rbac.yaml
cat $SP/istio-install/istio-mixer.yaml >> $SP/istio-rbac.yaml
cat $SP/istio-rbac/istio-rbac.yaml >> $SP/istio-rbac.yaml
sed 's/# RBAC: //' $SP/istio-install/istio-manager.yaml >> $SP/istio-rbac.yaml
sed 's/# RBAC: //' $SP/istio-install/istio-mixer.yaml >> $SP/istio-rbac.yaml
