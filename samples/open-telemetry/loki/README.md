# Open Telemetry with Loki

This sample demonstrates Istio's Open Telemetry [ALS(Access Log Service)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/access_loggers/grpc/v3/als.proto) and sending logs to [Loki](https://github.com/grafana/loki).

## Install Istio

Run the following script to install Istio with an Open Telemetry ALS provider:

```bash
istioctl install -f iop.yaml -y
```

## Setup Loki

Run the following script to install `Loki`:

```bash
kubectl apply -f ../../addons/loki.yaml -n istio-system
```

## Setup otel-collector service

First, create an `otel-collector` backend with a simple configuration.

```bash
kubectl apply -f otel.yaml -n istio-system
```

With the following configuration, otel-collector-contrib will create a grpc receiver on port `4317`, and output to stdout. You can find more details [here](https://github.com/open-telemetry/opentelemetry-collector-contrib).

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

Following this [doc](../../httpbin/README.md), start the `fortio` and `httpbin` services.

Run the following script to request `httpbin` from `fortio`.

```bash
kubectl exec -it deploy/fortio -- fortio curl httpbin:8000/ip
```

Run the following script to view ALS output.

```bash
kubectl logs -l app=opentelemetry-collector -n istio-system --tail=-1
```

You can also check the Grafana logs:

```bash
istioctl dashboard grafana
```

Learn how to use Loki with Grafana [here](https://grafana.com/docs/grafana/v8.4/datasources/loki/).

## Cleanup

```bash
kubectl delete -f otel.yaml -n istio-system
kubectl delete telemetry mesh-logging -n istio-system
istioctl uninstall --purge -y
```
