#!/bin/bash -e
set -o pipefail

NAMESPACE=poc

kubectl apply -f - << EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $NAMESPACE
EOF

kubectl apply -n $NAMESPACE -f config.yaml
kubectl apply -n $NAMESPACE -f client.yaml
kubectl apply -n $NAMESPACE -f client-l7.yaml

CLIENT_L7_SVC_IP=$(kubectl get svc -n $NAMESPACE client-l7 -ojsonpath={.spec.clusterIP})

rm -f client-uproxy.yaml
cat > client-uproxy.yaml << EOF
static_resources:
  listeners:
  - address:
      socket_address:
        address: 0.0.0.0
        port_value: 15001
    listener_filters:
    - name: original_dst
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.listener.original_dst.v3.OriginalDst
    filter_chains:
    - filters:
      - name: tcp_proxy
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
          cluster: remote_outbound
          stat_prefix: remote_outbound
  clusters:
  - connect_timeout: 1s
    name: remote_outbound
    load_assignment:
      cluster_name: remote_outbound
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: $CLIENT_L7_SVC_IP
                port_value: 80
    transport_socket:
      name: envoy.transport_sockets.upstream_proxy_protocol
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.transport_sockets.proxy_protocol.v3.ProxyProtocolUpstreamTransport
        config:
          version: V1
        transport_socket:
          name: envoy.transport_sockets.raw_buffer
admin:
  access_log_path: /dev/null
  address:
    socket_address:
      address: 127.0.0.1
      port_value: 15000
node:
  id: client-uproxy
  cluster: uproxy
EOF

kubectl create cm -n $NAMESPACE client-uproxy --from-file config.yaml=client-uproxy.yaml \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl delete po -n $NAMESPACE -l app=client

kubectl rollout status -n $NAMESPACE deploy client --timeout 15s
kubectl rollout status -n $NAMESPACE deploy client-l7 --timeout 15s
kubectl rollout status -n $NAMESPACE deploy server --timeout 15s

CLIENT_NAME=$(kubectl get pod -n $NAMESPACE -l app=client -oname)

kubectl exec -itn $NAMESPACE $CLIENT_NAME -c sleep -- curl server
