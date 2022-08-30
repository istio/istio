# Open Telemetry ALS

This sample demonstrates Istio's Open Telemetry ALS support.

## Start otel-collector service

First, create an `otel-collector` backend with simple configuration.

```bash
kubectl apply -f otel.yaml -nistio-system
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

## Update istio configmap

Run the following script to update the `istio` with demo profile:

```bash
istioctl install --set profile=demo -y
```

Next, add a Telemetry resource that tells Istio to send access logs to the OpenTelemetry collector.

```bash
cat <<EOF | kubectl apply -n istio-system -f -
apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: mesh-default
  namespace: istio-system
spec:
  accessLogging:
    - providers:
      - name: otel
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
kubectl delete -f otel.yaml -nistio-system
kubectl delete telemetry mesh-default -nistio-system
```