# Custom Envoy Bootstrap Configuration

This sample creates a simple helloworld service that bootstraps the Envoy proxy. There are 2 ways to implement this:
1. With a custom configuration file. 
2. With envoy filter

## Starting the service

First, we need to create a `ConfigMap` resource with our bootstrap configuration.

```bash
kubectl apply -f custom-bootstrap.yaml
kubectl apply -f custom-bootstrap-envoy-filter.yaml
```

Next, we can create a service that uses this bootstrap configuration.

- For using `custom-bootstrap.yaml` we need to add an annotation, `sidecar.istio.io/bootstrapOverride` to pod spec with the name of our ConfigMap as the value.
- For using `custom-bootstrap-envoy-filter.yaml`, we need to enable `BOOTSTRAP_XDS_AGENT` [environment variable](https://istio.io/latest/docs/reference/commands/pilot-agent/#envvars).

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

## Customizing the Bootstrap

The configuration provided will be passed to envoy using the [`--config-yaml`](https://www.envoyproxy.io/docs/envoy/v1.7.1/operations/cli#cmdoption-config-yaml) flag.

This will merge the passed in configuration with the default configuration. Singular values will replace the default values, while repeated values will be appended.

For reference, [the default bootstrap configuration](../../tools/packaging/common/envoy_bootstrap.json) and Envoy's [configuration reference](https://www.envoyproxy.io/docs/envoy/latest/configuration/configuration#config) may be useful

## Cleanup

```bash
kubectl delete -f custom-bootstrap.yaml
kubectl delete -f custom-bootstrap-envoy-filter.yaml
kubectl delete -f example-app.yaml
```
