# Istio Operator

The istio/operator repo is part of istio/istio from 1.5 onwards.
You can [contribute](../CONTRIBUTING.md) by picking an
[unassigned open issue](https://github.com/istio/istio/issues?q=is%3Aissue+is%3Aopen+label%3Aarea%2Fenvironments%2Foperator+no%3Aassignee),
creating a [bug or feature request](../BUGS-AND-FEATURE-REQUESTS.md),
or just coming to the weekly [Environments Working Group](https://github.com/istio/community/blob/master/WORKING-GROUPS.md)
meeting to share your ideas.

This document is an overview of how the operator works from a user perspective. For more details about the design and
architecture and a code overview, see [ARCHITECTURE.md](../architecture/environments/operator.md).

## Introduction

The operator formerly acted as an in-cluster operator, dynamically reconciling an Istio installation.
This mode has now been removed, and it only serves as a client-side CLI tool to install Istio.

The operator uses the [IstioOperator API](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto), which has
three main components:

- [MeshConfig](https://github.com/istio/api/blob/master/mesh/v1alpha1/config.proto) for runtime config consumed directly by Istio
control plane components.
- [Component configuration API](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto#L42-L93), for managing
K8s settings like resources, auto scaling, pod disruption budgets and others defined in the
[KubernetesResourceSpec](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto#L217-L271)
for Istio core and addon components.
- The legacy
[Helm installation API](https://github.com/istio/istio/blob/master/operator/pkg/apis/istio/v1alpha1/values_types.proto) for backwards
compatibility.

Some parameters will temporarily exist both the component configuration and legacy Helm APIs - for example, K8s
resources. However, the Istio community recommends using the first API as it is more consistent, is validated,
and will naturally follow the graduation process for APIs while the same parameters in the configuration API are planned
for deprecation.

[Profiles](https://istio.io/docs/setup/kubernetes/additional-setup/config-profiles/), are provided as a starting point for
an Istio install and can be customized by creating customization overlay files or passing parameters through the
--set flag. For example, to select the minimal profile:

```yaml
# minimal.yaml

apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: minimal
```

If you don't specify a configuration profile, Istio is installed using the `default` configuration profile. All
profiles listed in istio.io are available by default, or `profile:` can point to a local file path to reference a custom
profile base to use as a starting point for customization. See the [API reference](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto)
for details.

## Developer quick start

The quick start describes how to install and use the operator `mesh` CLI command and/or controller.

### CLI

To build the operator CLI, simply:

```bash
make build
```

Ensure the created binary is in your PATH to run the examples below.

### Quick tour of CLI commands

#### Flags

The `istioctl` command supports the following flags:

- `dry-run`: console output only, nothing applied to cluster or written to files.
- `verbose`: display entire manifest contents and other debug info (default is false).
- `set`: select profile or override profile defaults

#### Basic default manifest

The following command generates a manifest with the compiled-in `default` profile and charts:

```bash
istioctl manifest generate
```

You can see these sources for the compiled-in profiles and charts in the repo under `manifests/`. These profiles and charts are also included in the Istio release tar.

#### Output to dirs

The output of the manifest is concatenated into a single file. To generate a directory hierarchy with subdirectory
levels representing a child dependency, use the following command:

```bash
istioctl manifest generate -o istio_manifests
```

Use depth first search to traverse the created directory hierarchy when applying your YAML files. This is needed for
correct sequencing of dependencies. Child manifest directories must wait for their parent directory to be fully applied,
but not their sibling manifest directories.

#### Just apply it for me

The following command generates the manifests and applies them in the correct dependency order, waiting for the
dependencies to have the needed CRDs available:

```bash
istioctl install
```

#### Review the values of a configuration profile

The following commands show the values of a configuration profile:

```bash
# show available profiles
istioctl profile list

# show the values in demo profile
istioctl profile dump demo

# show the values after a customization file is applied
istioctl profile dump -f samples/pilot-k8s.yaml

# show the differences in the generated manifests between the default profile and a customized install
istioctl manifest generate > 1.yaml
istioctl manifest generate -f samples/pilot-k8s.yaml > 2.yaml
istioctl manifest diff 1.yaml 2.yaml
```

The profile dump sub-command supports a couple of useful flags:

- `config-path`: select the root for the configuration subtree you want to see e.g. just show Pilot:

```bash
istioctl profile dump --config-path components.pilot
```

- `filename`: set parameters in the configuration file before dumping the resulting profile e.g. show the pilot k8s overlay settings:

```bash
istioctl profile dump --filename samples/pilot-k8s.yaml
```

#### Select a specific configuration profile

The simplest customization is to select a profile different to `default` e.g. `minimal`. See [manifests/profiles/minimal.yaml](../manifests/profiles/minimal.yaml):

```yaml
# minimal-install.yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: minimal
```

Use `istioctl` to generate the manifests for the new configuration profile:

```bash
istioctl manifest generate -f manifests/profiles/minimal.yaml
```

After running the command, the Helm charts are rendered using `manifests/profiles/minimal.yaml`.

##### --set syntax

The CLI `--set` option can be used to override settings within the profile.

For example, to enable auto mTLS, use `istioctl manifest generate --set values.global.mtls.auto=true --set values.global.controlPlaneSecurityEnabled=true`

To override a setting that includes dots, escape them with a backslash (\).  Your shell may require enclosing quotes.

``` bash
istioctl manifest generate --set "values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy=runtime/default"
```

To override a setting that is part of a list, use brackets.

``` bash
istioctl manifest generate --set values.gateways.istio-ingressgateway.enabled=false \
--set values.gateways.istio-egressgateway.enabled=true \
--set 'values.gateways.istio-egressgateway.secretVolumes[0].name'=egressgateway-certs \
--set 'values.gateways.istio-egressgateway.secretVolumes[0].secretName'=istio-egressgateway-certs \
--set 'values.gateways.istio-egressgateway.secretVolumes[0].mountPath'=/etc/istio/egressgateway-certs
```

#### Install from file path

The compiled in charts and profiles are used by default, but you can specify a file path, for example:

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: /usr/home/bob/go/src/github.com/ostromart/istio-installer/data/profiles/default.yaml
  installPackagePath: /usr/home/bob/go/src/github.com/ostromart/istio-installer/data/charts/
```

You can mix and match these approaches. For example, you can use a compiled-in configuration profile with charts in your
local file system.

#### Check diffs of manifests

The following command takes two manifests and output the differences in a readable way. It can be used to compare between the manifests generated by operator API and helm directly:

```bash
istioctl manifest diff ./out/helm-template/manifest.yaml ./out/mesh-manifest/manifest.yaml
```

### New API customization

The [new platform level installation API](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto)
defines install time parameters like component and enablement and namespace, and K8s settings like resources, HPA spec etc. in a structured way.
The simplest customization is to turn components on and off. For example, to turn on cni ([samples/cni-on.yaml](samples/cni-on.yaml)):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    cni:
      enabled: true
```

The operator validates the configuration and automatically detects syntax errors. If you are
using Helm values that are incompatible, the schema validation used in the operator may reject input that is valid for
Helm.
Each Istio component has K8s settings, and these can be overridden from the defaults using official K8s APIs rather than
Istio defined schemas ([samples/pilot-k8s.yaml](samples/pilot-k8s.yaml)):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    pilot:
      k8s:
        resources:
          requests:
            cpu: 1000m # override from default 500m
            memory: 4096Mi # ... default 2048Mi
        hpaSpec:
          maxReplicas: 10 # ... default 5
          minReplicas: 2  # ... default 1
        nodeSelector: # ... default empty
          master: "true"
        tolerations: # ... default empty
        - key: dedicated
          operator: Exists
          effect: NoSchedule
        - key: CriticalAddonsOnly
          operator: Exists
```

The K8s settings are defined in detail in the
[operator API](https://github.com/istio/api/blob/00671adacbea20f941cb20cce021bc63cbad1840/operator/v1alpha1/operator.proto).
The settings are the same for all components, so a user can configure pilot K8s settings in exactly the same, consistent
way as galley settings. Supported K8s settings currently include:

- [resources](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#resource-requests-and-limits-of-pod-and-container)
- [readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/)
- [replica count](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [HorizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
- [PodDisruptionBudget](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#how-disruption-budgets-work)
- [pod annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [container environment variables](https://kubernetes.io/docs/tasks/inject-data-application/define-environment-variable-container/)
- [ImagePullPolicy](https://kubernetes.io/docs/concepts/containers/images/)
- [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass)
- [node selector](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector)
- [toleration](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/)
- [affinity and anti-affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity)
- [deployment strategy](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [service annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [service spec](https://kubernetes.io/docs/concepts/services-networking/service/)
- [pod securityContext](https://kubernetes.io/docs/tasks/configure-pod-container/security-context/#set-the-security-context-for-a-pod)

All of these K8s settings use the K8s API definitions, so [K8s documentation](https://kubernetes.io/docs/concepts/) can
be used for reference. All K8s overlay values are also validated in the operator.

### Customizing the old values.yaml API

The new platform install API above deals with K8s level settings. The remaining values.yaml parameters deal with Istio
control plane operation rather than installation. For the time being, the operator just passes these through to the Helm
charts unmodified (but validated through a
[schema](pkg/apis/values_types.proto)). Values.yaml settings
are overridden the same way as the new API, though a customized CR overlaid over default values for the selected
profile. Here's an example of overriding some global level default values ([samples/values-global.yaml](samples/values-global.yaml)):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: sds
  values:
    global:
      logging:
        level: "default:warning" # override from info
```

Values overrides can also be specified for a particular component
 ([samples/values-pilot.yaml](samples/values-pilot.yaml)):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    pilot:
      traceSampling: 0.1 # override from 1.0
```

### Advanced K8s resource overlays

Advanced users may occasionally have the need to customize parameters (like container command line flags) which are not
exposed through either of the installation or configuration APIs described in this document. For such cases, it's
possible to overlay the generated K8s resources before they are applied with user-defined overlays. For example, to
override some container level values in the Pilot container  ([samples/pilot-advanced-override.yaml](samples/pilot-advanced-override.yaml)):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  components:
    pilot:
      k8s:
        overlays:
        - kind: Deployment
          name: istio-pilot
          patches:
          - path: spec.template.spec.containers.[name:discovery].args.[30m]
            value: "60m" # OVERRIDDEN
          - path: spec.template.spec.containers.[name:discovery].ports.[containerPort:8080].containerPort
            value: 8090 # OVERRIDDEN
          - path: 'spec.template.spec.volumes[100]' #push to the list
            value:
              configMap:
                name: my-config-map
              name: my-volume-name
          - path: 'spec.template.spec.containers[0].volumeMounts[100]'
            value:
              mountPath: /mnt/path1
              name: my-volume-name
        - kind: Service
          name: istio-pilot
          patches:
          - path: spec.ports.[name:grpc-xds].port
            value: 15099 # OVERRIDDEN
```

The user-defined overlay uses a path spec that includes the ability to select list items by key. In the example above,
the container with the key-value "name: discovery" is selected from the list of containers, and the command line
parameter with value "30m" is selected to be modified. The advanced overlay capability is described in more detail in
the spec.

## Architecture

See [ARCHITECTURE.md](../architecture/environments/operator.md)
