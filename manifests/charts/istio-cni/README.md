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

## Configuration

To view supported configuration options and documentation, run:

```console
helm show values istio/istio-cni
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

To enable ambient, you can use the ambient profile: `--set profile=ambient`.

#### Calico

For Calico, you must also modify the settings to allow source spoofing:

- if deployed by operator,  `kubectl patch felixconfigurations default --type='json' -p='[{"op": "add", "path": "/spec/workloadSourceSpoofing", "value": "Any"}]'`
- if deployed by manifest, add env `FELIX_WORKLOADSOURCESPOOFING` with value `Any` in `spec.template.spec.containers.env` for daemonset `calico-node`. (This will allow PODs with specified annotation to skip the rpf check. )

### GKE notes

On GKE, 'kube-system' is required.

If using `helm template`, `--set cni.cniBinDir=/home/kubernetes/bin` is required - with `helm install`
it is auto-detected.

Helm 3.19.0+ breaks auto-detection due to version normalization. Use `--set profile=platform-gke` or manually set `--set cni.cniBinDir=/home/kubernetes/bin`.
