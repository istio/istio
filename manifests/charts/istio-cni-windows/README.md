# Istio CNI Helm Chart

This chart installs the Istio CNI Plugin for Windows. See the [CNI installation guide](https://istio.io/latest/docs/setup/additional-setup/cni/)
for more information.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

To install the chart with the release name `istio-cni`:

```console
helm install istio-cni-windows istio/cni-windows -n kube-system
```

## Configuration

To view support configuration options and documentation, run:

```console
helm show values istio/istio-cni-windows
```

### Profiles

Istio Helm charts have a concept of a `profile`, which is a bundled collection of value presets.
These can be set with `--set profile=<profile>`.
For example, the `demo` profile offers a preset configuration to try out Istio in a test environment, with additional features enabled and lowered resource requirements.

For consistency, the same profiles are used across each chart, even if they do not impact a given chart.

Explicitly set values have highest priority, then profile settings, then chart defaults.

As an implementation detail of profiles, the default values for the chart are all nested under `defaults`.
When configuring the chart, you should not include this.
That is, `--set some.field=true` should be passed, not `--set defaults.some.field=true`.

### Ambient

Windows support is only available in the ambient profile. To enable ambient, you can use the ambient profile: `--set profile=ambient`.

