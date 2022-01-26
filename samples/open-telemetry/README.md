# Open Telemetry ALS

This sample demonstrates Istio's Open Telemetry ALS support.

## Start otel-collector service

First, create an `otel-collector` backend with simple configuration.

```bash
kubectl apply -f otel.yaml
```

With following configuration, otel-collector will create a grpc receiver on port `4317`, and output to stdout. You can find more details form [here](https://github.com/open-telemetry/opentelemetry-collector).

```yaml
receivers:
      otlp:
        protocols:
          grpc:
          http:
    processors:
      batch:
    exporters:
      logging:
        loglevel: debug
    service:
      pipelines:
        logs:
          receivers: [otlp]
          processors: [batch]
          exporters: [logging]
```

## Modiy istio configmap

Run the following script to edit the istio MeshConfig, update the YAML files in one step.

```bash
cat <<EOF | kubectl apply -n istio-system -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: istio
  namespace: istio-system
data:
  mesh: |-
    accessLogFile: /dev/stdout
    defaultConfig:
      discoveryAddress: istiod.istio-system.svc:15012
      proxyMetadata: {}
      tracing:
        zipkin:
          address: zipkin.istio-system:9411
    enablePrometheusMerge: true
    extensionProviders:
    - name: otel
      envoyOtelAls:
        service: otel-collector.istio-system.svc.cluster.local
        port: 4317
        logFormat:
          labels:
            "protocol": "%PROTOCOL%"
            "attempt": "%REQ(X-ENVOY-ATTEMPT-COUNT)%"
    defaultProviders:
      accessLogging:
      - envoy
      - otel
    rootNamespace: istio-system
    trustDomain: cluster.local
  meshNetworks: 'networks: {}'
EOF
```

## Check ALS output

Following [doc](../httpbin/README.md), start the `fortio` and `httpbin` services.

Run the following script to request `httpbin` from `fortio` .

```bash
kubectl exec -it $(kubectl get po | grep fortio | awk '{print $1}') -- fortio curl httpbin:8000/ip
```

Run the following script to checkout ALS output.

```bash
kubectl logs $(kubectl get po -n istio-system | grep otel | awk '{print $1}') -n istio-system
```

## Cleanup

```bash
kubectl delete -f otel.yaml
```