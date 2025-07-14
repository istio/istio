# Istio Ztunnel Helm Chart

This chart installs an Istio ztunnel for Windows Server.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

To install the chart:

```console
helm install ztunnel-windows istio/ztunnel-windows
```

## Uninstalling the Chart

To uninstall/delete the chart:

```console
helm delete ztunnel-windows
```

## Configuration

To view support configuration options and documentation, run:

```console
helm show values istio/ztunnel-windows
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
