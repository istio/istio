#!/bin/bash

helmExtraArgs=$1

message="Deploying Istio"

if [ "x$helmExtraArgs" != "x" ]; then
message=$message" with extra args: "$helmExtraArgs
fi
echo $message 

echo "Creating RBAC rules for Istio..."

helm install istio-rbac --name istio-rbac
retCode=$?
if [ $retCode != '0' ]; then
  echo "Creating of RBAC rules failed, exiting..."
  exit $retCode 
fi

echo "Creating a namespace for Istio..."

helm install istio-namespace --name istio-namespace
retCode=$?
if [ $retCode != '0' ]; then
  echo "Creating of namespace failed, exiting..."
  exit $retCode
fi

echo "Creating CRD objects for Istio..."

helm install istio-crds --name istio-crds
retCode=$?
if [ $retCode != '0' ]; then
  echo "Creating of CRDs objects failed, exiting..."
  exit $retCode
fi

echo "Deploying Istio..."

helm install istio --name istio $helmExtraArgs
retCode=$?
if [ $retCode != '0' ]; then
  echo "Deploying Istio failed, exiting..."
  exit $retCode
fi

echo "Istio charts have been deployed, please check the status of PODs..."
exit 0
