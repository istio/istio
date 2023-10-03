# Open Telemetry Tracing

This sample demonstrates the support for the OpenTelemetry tracing provider with the Telemetry API.

## Start otel-collector service

First, deploy the `otel-collector` backend with simple configuration.

```bash
kubectl -n <namespace> apply -f ../otel.yaml
```

In this example, we use `otel-collector` as the namespace to deploy the `otel-collector` backend:

```bash
kubectl create namespace otel-collector
kubectl -n otel-collector apply -f ../otel.yaml
```

The otel-collector will create a grpc receiver on port `4317`, and later the sidecars will report trace information to this grpc port. You can find more details from [here](https://github.com/open-telemetry/opentelemetry-collector).

Below is the configuration:

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

In this example, `Jaeger` is the exporter for gathering the traces. Assuming you have already deployed Jaeger as your tracing system with [this](https://istio.io/latest/docs/ops/integrations/jaeger/) installation, you are good to go to the next steps. If you already have your own `Jaeger` deployed, you may need to modify the otel collector config. The configmap name is `opentelemetry-collector-conf` in the namespace you deployed the otel collector, and the related config is defined as:

```yaml
exporters:
  jaeger:
    endpoint: jaeger-collector.istio-system.svc.cluster.local:14250
    tls:
      insecure: true
    sending_queue:
      enabled: true
    retry_on_failure:
      enabled: true
service:
  pipelines:
    traces:
      exporters:
      - jaeger
```

You need to modify the jaeger exporter endpoint with the one you deployed, in this case it's `jaeger-collector.istio-system.svc.cluster.local:14250`.

If you have not deployed the `Jaeger` service, you can follow [this](https://istio.io/latest/docs/ops/integrations/jaeger/) installation to install the service.

You may also choose any existing tracing system if you have, and you should change the exporter settings in the configmap mentioned above.

You may also choose to use your own otel collector if you have, and the key part is to have the `otlp` grpc protocol receiver to receive the traces. One important thing is to make sure your otel collector service's grpc port starts with `grpc-` prefix, which is like:

```yaml
spec:
  ports:
    - name: grpc-otlp
      port: 4317
      protocol: TCP
      targetPort: 4317
```

Otherwise the traces may not be reported.

## Update mesh config

Install or update Istio with the `demo` profile to make sure you have the OpenTelemetry tracing provider enabled:

```bash
istioctl install --set profile=demo -y
```

Or ensure you have the following additional mesh config set in your Istio:

```yaml
mesh: |-
  extensionProviders:
  - name: otel-tracing
    opentelemetry:
      port: 4317
      service: opentelemetry-collector.otel-collector.svc.cluster.local
```

Make sure the service name matches the one you deployed if you select a different namespace.

## Apply the Telemetry resource to report traces

Next, add a Telemetry resource that tells Istio to send trace records to the OpenTelemetry collector.

```bash
kubectl -n <namespace> apply -f ./telemetry.yaml
```

In this example, we deploy it to the default namespace, which is where the sample apps
from the [getting started](https://istio.io/latest/docs/setup/getting-started) are also deployed.

```bash
kubectl apply -f ./telemetry.yaml
```

The core config is:

```yaml
tracing:
- providers:
  - name: otel-tracing
  randomSamplingPercentage: 0
```

As you see, the `randomSamplingPercentage` is 0, which means the tracing is still not enabled because of `0` sampling percentage. The tracing can be opt-on by increasing the `randomSamplingPercentage` value to `1-100`. The `Telemetry` resource can also be manipulated in workload/namespace/global levels, you can check [here](https://istio.io/latest/docs/reference/config/telemetry/) for more config examples.

## Check tracing results

If you have followed [this](https://istio.io/latest/docs/setup/getting-started/) getting started steps, you have the sample bookinfo applications installed. Try to make some requests to the productpage to generate some traces.

Then open up the `Jaeger` dashboard with:

```bash
istioctl dashboard jaeger
```

You will see the requests' trace records.

## Cleanup

```bash
kubectl -n otel-collector delete -f ./telemetry.yaml
kubectl -n otel-collector delete -f ../otel.yaml
```
