# Istiod Helm Chart

This chart installs an Istiod deployment.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

Before installing, ensure CRDs are installed in the cluster (from the `istio/base` chart).

To install the chart with the release name `istiod`:

```console
kubectl create namespace istio-system
helm install istiod istio/istiod --namespace istio-system
```

## Uninstalling the Chart

To uninstall/delete the `istiod` deployment:

```console
helm delete istiod --namespace istio-system
```

## Configuration

To view support configuration options and documentation, run:

```console
helm show values istio/istiod
```

### Examples

#### Configuring mesh configuration settings

Any [Mesh Config](https://istio.io/latest/docs/reference/config/istio.mesh.v1alpha1/) options can be configured like below:

```yaml
meshConfig:
  accessLogFile: /dev/stdout
```

#### Revisions

Control plane revisions allow deploying multiple versions of the control plane in the same cluster.
This allows safe [canary upgrades](https://istio.io/latest/docs/setup/upgrade/canary/)

```yaml
revision: my-revision-name
```
