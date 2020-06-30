# Custom Envoy Bootstrap Configuration

This sample creates a simple helloworld service that bootstraps the Envoy proxy with a custom configuration file.

## Starting the service

First, we need to create a `ConfigMap` resource with our bootstrap configuration.

```bash
kubectl apply -f custom-bootstrap.yaml
```

Next, we can create a service that uses this bootstrap configuration.

To do this, we need to add an annotation, `sidecar.istio.io/bootstrapOverride`, with the name of our ConfigMap as the value.

We can create our helloworld app, using the custom config, with:

```bash
kubectl apply -f example-app.yaml
```

If you don't have [automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection)
set in your cluster you will need to manually inject it to the services instead:

```bash
istioctl kube-inject -f example-app.yaml -o example-app-istio.yaml
kubectl apply -f example-app-istio.yaml
```

## Checking the Bootstrap Configuration

To see what bootstrap configuration a pod is using:

```bash
istioctl proxy-config bootstrap <POD-NAME>
```

## Customizing Gateway

`custom-bootstrap-runtime.yaml` contains an example of customizing `runtime` properties in Envoy.
Create a config map in the `istio-system` namespace.

```bash
kubectl -n istio-system apply -f custom-bootstrap-runtime.yaml
```

Istio gateways do not use sidecar injector, so we need patch the gateway deployments manually.

`gateway-patch.yaml` file performs the following steps.

1. Attach the `boostrapOverride` config map to the gateway pod.
2. Mount the configmap at a wellknown path (/etc/istio/custom-bootstrap/custom_bootstrap.json).
3. Set `ISTIO_BOOTSTRAP_OVERRIDE` env variable to the above path.

You can patch a gateway deployment using the following command.

```bash
kubectl --namespace istio-system patch deployment istio-ingressgateway --patch "$(cat gateway-patch.yaml)"
```

## Customizing the Bootstrap

The configuration provided will be passed to envoy using the [`--config-yaml`](https://www.envoyproxy.io/docs/envoy/v1.7.1/operations/cli#cmdoption-config-yaml) flag.

This will merge the passed in configuration with the default configuration. Singular values will replace the default values, while repeated values will be appended.

For reference, [the default bootstrap configuration](/tools/packaging/common/envoy_bootstrap.json) and Envoy's [configuration reference](https://www.envoyproxy.io/docs/envoy/latest/configuration/configuration#config) may be useful

## Cleanup

```bash
kubectl delete -f custom-bootstrap.yaml
kubectl delete -f example-app.yaml
```
