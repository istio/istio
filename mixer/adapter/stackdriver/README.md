# Mixer Stackdriver Adapter

To enable stackdriver adapter:

1.  Download istio/installer repo:

    ```bash
    git clone https://github.com/istio/installer && cd installer
    ```

1.  Render and apply Stackdriver adapter manifest:

    ```bash
    helm template istio-telemetry/mixer-telemetry --execute=templates/stackdriver.yaml -f global.yaml --set mixer.adapters.stackdriver.enabled=true --namespace istio-system | kubectl apply -f -
    ```

The full Stackdriver adapter template can be found [here](https://github.com/istio/installer/blob/master/istio-telemetry/mixer-telemetry/templates/stackdriver.yaml) with several values to set:

```yaml
mixer:
  stackdriver:
    enabled: false

  auth:
    appCredentials: false
    apiKey: ""
    serviceAccountPath: ""

  tracer:
    enabled: false
    sampleProbability: 1
```
