# Custom Envoy Bootstrap Configuration

This sample creates a simple helloworld service that bootstraps the Envoy proxy with a custom configuration file.

It may be useful to reference, [the default bootstrap configuration](/tools/deb/envoy_bootstrap_v2.json) and Envoy's [configuration reference](https://www.envoyproxy.io/docs/envoy/latest/configuration/configuration#config).

## Running The Example

### Creating The Configuration
First, we need to create a `ConfigMap` resource with our custom bootstrap configuration.

This `ConfigMap` has a value, `bootstrap_override.json`, which has the bootstrap configuration to use.

```bash
kubectl apply -f custom-bootstrap.yaml
```

### Creating The Service

Next, we can create a service that uses this bootstrap configuration.

To do this, we need to add an annotation with the name of our ConfigMap as the value.

In our case, this looks like `sidecar.istio.io/bootstrapOverride: "istio-bootstrap-override-config"`

We can create our helloworld app, using the custom config, with:

```bash
kubectl apply -f example-app.yaml
```

If you don't have [automatic sidecar injection](https://istio.io/docs/setup/kubernetes/sidecar-injection.html#automatic-sidecar-injection)
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

Using this command we can verify that our custom configuration was applied as expected. For this example, we would expect to see that:

* `node.metadata` is `"custom": "My Custom Metadata"`

Note that there is nothing special about these values - they are simply examples chosen.

## Cleanup

```bash
kubectl delete -f custom-bootstrap.yaml
kubectl delete -f example-app.yaml
```