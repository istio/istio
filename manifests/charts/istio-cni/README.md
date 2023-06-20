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
`priorityClassName` can be used. You can install in other namespace only on K8S clusters that allow
'system-node-critical' outside of kube-system.

## Ambient

To enable ambient, you need to add `--set cni.ambient.enabled=true`.

### Calico

For Calico, you must also modify the settings to allow source spoofing:

- if deployed by operator,  `kubectl patch felixconfigurations default --type='json' -p='[{"op": "add", "path": "/spec/workloadSourceSpoofing", "value": "Any"}]'`
- if deployed by manifest, add env `FELIX_WORKLOADSOURCESPOOFING` with value `Any` in `spec.template.spec.containers.env` for daemonset `calico-node`. (This will allow PODs with specified annotation to skip the rpf check. )

## GKE notes

On GKE, 'kube-system' is required.

If using `helm template`, `--set cni.cniBinDir=/home/kubernetes/bin` is required - with `helm install`
it is auto-detected.
