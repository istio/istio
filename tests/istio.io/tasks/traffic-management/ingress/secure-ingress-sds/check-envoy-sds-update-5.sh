#!/bin/bash

for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 5" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 5 but got ${stats}" >&2
