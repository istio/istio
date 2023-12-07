# Updating charts and values.yaml

## Acceptable Pull Requests

Helm charts `values.yaml` represent a complex user facing API that tends to grow uncontrollably over time
due to design choices in Helm.
The underlying Kubernetes resources we configure have 1000s of fields; given enough users and bespoke use cases,
eventually someone will want to customize every one of those fields.
If all fields are exposed in `values.yaml`, we end up with an massive API that is also likely worse than just using the Kubernetes API directly.

To avoid this, the project attempts to minimize additions to the `values.yaml` API where possible.

If the change is a dynamic runtime configuration, it probably belongs in the [MeshConfig API](https://github.com/istio/api/blob/master/mesh/v1alpha1/config.proto).
This allows configuration without re-installing or restarting deployments.

If the change is to a Kubernetes field (such as modifying a Deployment attribute), it will likely need to be install-time configuration.
However, that doesn't necessarily mean a PR to add a value will be accepted.
The `values.yaml` API is intended to maintain a *minimal core set of configuration* that most users will use.
For bespoke use cases, [Helm Chart Customization](https://istio.io/latest/docs/setup/additional-setup/customize-installation-helm/#advanced-helm-chart-customization) can be used
to allow arbitrary customizations.

If the change truly is generally purpose, it is generally preferred to have broader APIs. For example, instead of providing
direct access to each of the complex fields in [affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/), just providing
a single `affinity` field that is passed through as-is to the Kubernetes resource.
This provides maximum flexibility with minimal API surface overhead.

## Making changes

## Step 1. Make changes in charts and values.yaml in `manifests` directory

Be sure to provide sufficient documentation and example usage in values.yaml.
If the chart has a `values.schema.json`, that should be updated as well.

## Step 2. Update the istioctl/Operator values

If you are modifying the `gateway` chart, you can stop here.
All other charts, however, are exposed by `istioctl` and need to follow the steps below.

The charts in the `manifests` directory are used in istioctl to generate an installation manifest.

If `values.yaml` is changed, be sure to update corresponding values changes in [../profiles/default.yaml](../profiles/default.yaml)

## Step 3. Update istioctl schema

Istioctl uses a [schema](../../operator/pkg/apis/istio/v1alpha1/values_types.proto) to validate the values. Any changes to
the schema must be added here, otherwise istioctl users will see errors.
Once the schema file is updated, run:

```bash
$ make operator-proto
```

This will regenerate the Go structs used for schema validation.

## Step 4. Update the generated manifests

Tests of istioctl use the auto-generated manifests to ensure that the istioctl binary has the correct version of the charts.
To regenerate the manifests, run:

```bash
$ make copy-templates update-golden
```

## Step 5. Create a PR using outputs from Steps 1 to 4

Your PR should pass all the checks if you followed these steps.
