# Istio Ztunnel Helm Chart

This chart installs an Istio ztunnel.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

To install the chart:

```console
helm install ztunnel istio/ztunnel
```

## Uninstalling the Chart

To uninstall/delete the chart:

```console
helm delete ztunnel
```

## Configuration

To view support configuration options and documentation, run:

```console
helm show values istio/ztunnel
```
