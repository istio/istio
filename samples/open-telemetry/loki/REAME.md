# Open Telemetry ALS with Loki

This sample demonstrates Istio's Open Telemetry ALS and send logs to [Loki](https://github.com/grafana/loki).

## Install Istio

Run the following script to install `istio` with Open Telemetry ALS provider:

```bash
istioctl install -f iop.yaml -y
```

## Setup otel-collector service

First, create an `otel-collector` backend with simple configuration.

```bash
kubectl apply -f otel.yaml -n istio-system
```

With following configuration, otel-collector-contrib will create a grpc receiver on port `4317`, and output to stdout. You can find more details from [here](https://github.com/open-telemetry/opentelemetry-collector-contrib).

```yaml
    receivers:
      otlp:
        protocols:
          grpc:
          http:
    processors:
      batch:
      attributes:
        actions:
        - action: insert
          key: loki.attribute.labels
          value: podName, namespace,cluster,meshID
    exporters:
      loki:
        endpoint: "http://loki.istio-system.svc:3100/loki/api/v1/push"
      logging:
        loglevel: debug
    extensions:
      health_check:
    service:
      extensions:
      - health_check
      pipelines:
        logs:
          receivers: [otlp]
          processors: [attributes]
          exporters: [loki, logging]
```

## Apply Telemetry API

Next, add a Telemetry resource that tells Istio to send access logs to the OpenTelemetry collector.

```bash
kubectl apply -f telemetry.yaml
```

## Check ALS output

Following [doc](../../httpbin/README.md), start the `fortio` and `httpbin` services.

Run the following script to request `httpbin` from `fortio`.

```bash
kubectl exec -it deploy/fortio -- fortio curl httpbin:8000/ip
```

Run the following script to checkout ALS output.

```bash
kubectl logs -l app=opentelemetry-collector -n istio-system --tail=-1
```

You can also check logs from Grafana:

```
istioctl dashboard grafana
```

Learn how to use Loki with Grafana from [here](https://grafana.com/docs/grafana/v8.4/datasources/loki/).

## Cleanup

```bash
kubectl delete -f otel.yaml -n istio-system
kubectl delete telemetry mesh-logging -n istio-system
istioctl uninstall --purge -y
```
