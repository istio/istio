# Custom Envoy Bootstrap Configuration

This sample creates a simple helloworld service that bootstraps the Envoy proxy with a custom configuration file.

## Configuration Options

There are two ways to configure the envoy bootstrap. For both options, it can be useful to reference, [the default bootstrap configuration](/tools/deb/envoy_bootstrap_v2.json) and Envoy's [configuration reference](https://www.envoyproxy.io/docs/envoy/latest/configuration/configuration#config) may be useful

### Configuration Merge

This option will apply overrides to the default Envoy bootstrap configuration, by merging the two configurations.

For details on how the merging works, see the documentation on the [`--config-yaml`](https://www.envoyproxy.io/docs/envoy/v1.7.1/operations/cli#cmdoption-config-yaml) Envoy flag, and [MergeFrom](https://developers.google.com/protocol-buffers/docs/reference/cpp/google.protobuf.message#Message.MergeFrom.details), which describes how this merging takes place.

Note that this option will *not* be processed by templating.

### Configuration Override

This option will completely overwrite the Envoy bootstrap configuration with your own configuration.

While this provides more flexibility, additional effort may be needed if the default configuration changes.

Note that this option will be processed by templating.

## Running The Example

In this example, we will use a fully custom config and an override. This likely will not be useful in real scenarios, but showcases both methods.

### Creating The Configuration
First, we need to create a `ConfigMap` resource with our bootstrap configuration.

This file contains two `ConfigMap`s - one for our override, with a filename of `bootstrap_override.json`, and another for our custom config, with a filename of `custom_boostrap.json`.

```bash
kubectl apply -f custom-bootstrap.yaml
```

### Creating The Service

Next, we can create a service that uses this bootstrap configuration.

To do this, we need to add an annotation with the name of our ConfigMap as the value.

* For a bootstrap merge: `sidecar.istio.io/bootstrapMerge`
* For a bootstrap override: `sidecar.istio.io/bootstrapOverride`

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
* `tracing.http.config.collector_endpoint` is `"/api/v1/spans/custom"`

Note that there is nothing special about these values - they are simply examples chosen.

## Cleanup

```bash
kubectl delete -f custom-bootstrap.yaml
kubectl delete -f example-app.yaml
```