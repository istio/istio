# Istio CNI Helm Chart

This chart installs the Istio CNI Plugin. See the [CNI installation guide](https://istio.io/latest/docs/setup/additional-setup/cni/)
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
helm install istio-cni istio/cni -n kube-system
```

Installation in `kube-system` is recommended to ensure the [`system-node-critical`](https://kubernetes.io/docs/tasks/administer-cluster/guaranteed-scheduling-critical-addon-pods/)
`priorityClassName` can be used.
