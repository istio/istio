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

#### Cilium with kubeproxyReplacement

When using Cilium CNI with `kubeproxyReplacement` enabled, readiness and liveness probes may fail in ambient mode. This occurs because eBPF programs in Cilium enforce the kubeproxyReplacement functionality, and the default link-local SNAT IP (169.254.7.127) is not routable in this scenario.

To resolve this, enable the `useHostIPForProbeSnat` option to use the host node's IP address for probe SNAT:

```bash
--set useHostIPForProbeSnat=true
```

Or in your values file:

```yaml
useHostIPForProbeSnat: true
```

This sets the `HOST_PROBE_SNAT_IP` environment variable to the host node's IP address, allowing probes to continue flowing from the kubelet to pods.

For more information, see [issue #57911](https://github.com/istio/istio/issues/57911).

### GKE notes

On GKE, 'kube-system' is required.

If using `helm template`, `--set cni.cniBinDir=/home/kubernetes/bin` is required - with `helm install`
it is auto-detected.
