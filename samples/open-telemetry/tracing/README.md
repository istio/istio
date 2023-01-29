# Open Telemetry Tracing

This sample demonstrates the support for the OpenTelemetry tracing provider with the Telemetry API.

## Start otel-collector service

First, deploy the `otel-collector` backend with simple configuration.

```bash
kubectl apply -f otel.yaml -n istio-system
```

The otel-collector will create a grpc receiver on port `4317`, and later the sidecars will report trace information to this grpc port. You can find more details from [here](https://github.com/open-telemetry/opentelemetry-collector).

The receiver is defined as the following configuration:

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

In this example, `zipkin` is the exporter for gathering the traces, which is defined as:

```yaml
exporters:
  zipkin:
    # Export to zipkin for easy querying
    endpoint: http://zipkin.istio-system.svc:9411/api/v2/spans
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
    traces:
      receivers:
      - otlp
      - opencensus
      exporters:
      - zipkin # add the exporter here
      - logging
```

If you have not deployed the `zipkin` service, you can use the following command:

```bash
kubectl apply -f ../../addons/extras/zipkin.yaml -n istio-system
```

You may also choose any existing tracing system if you have, and you should change the exporter settings in the otel-collector configmap.

## Update mesh config

Install or update Istio with the `demo` profile to make sure you have the OpenTelemetry tracing provider enabled:

```bash
istioctl install --set profile=demo -y
```

Or ensure you have the following additional mesh config set in your Istio:

```yaml
defaultConfig:
  enableTracing: true
  extensionProviders:
  - name: otel
    opentelemetry:
      port: 4317
      service: opentelemetry-collector.istio-system.svc.cluster.local
```

## Apply the Telemetry resource to report traces

Next, add a Telemetry resource that tells Istio to send trace records to the OpenTelemetry collector.

```yaml
kubectl apply -f telemetry.yaml -n istio-system
```

## Check tracing results

If you have followed [this](https://istio.io/latest/docs/setup/getting-started/) getting started steps, you have the sample bookinfo applications installed. Try to make some requests to the productpage to generate some traces.

Then open up the `zipkin` dashboard with:

```bash
istioctl dashboard zipkin
```

You will see the requests' trace records.

## Cleanup

```bash
kubectl delete -f telemetry.yaml -n istio-system
kubectl delete -f otel.yaml -n istio-system
```
