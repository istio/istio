#!/bin/bash
set -e
# Wait for all containers to be ready, for up to 2 minutes = 12 * 10 seconds.
retries=12
while [ "$retries" -gt "0" ]; do
  if kubectl get pods --all-namespaces -o jsonpath='{.items[?(@.status.phase != "Succeeded")].status.containerStatuses[*].ready}' | grep -q -v false; then
    exit 0
  fi
  echo "Waiting for containers to become ready"
  kubectl get pods --all-namespaces
  retries=$((retries - 1))
  sleep 10
done
exit 1
