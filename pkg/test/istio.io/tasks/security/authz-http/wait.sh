#!/bin/bash

for deploy in "productpage-v1" "details-v1" "ratings-v1" "reviews-v1" "reviews-v2" "reviews-v3" "sleep"; do
  kubectl -n default rollout status deployment "$deploy" --timeout 5m
  if [[ "$?" -ne 0 ]]; then
    echo "$deploy deployment rollout status check failed"
    exit 1
  fi
done

exit 0
