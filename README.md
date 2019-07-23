[![Go Report Card](https://goreportcard.com/badge/github.com/istio/operator)](https://goreportcard.com/report/github.com/istio/operator)
[![GolangCI](https://golangci.com/badges/github.com/istio/operator.svg)](https://golangci.com/r/github.com/istio/operator)

# Istio Operator

The Istio operator CLI is now suitable for developers to evaluate and experiment with. You can
[contribute](https://github.com/istio/operator/blob/master/CONTRIBUTING.md) by picking an
[unassigned open issue](https://github.com/istio/istio/issues?q=is%3Aissue+is%3Aopen+label%3Aarea%2Fenvironments%2Foperator+no%3Aassignee),
creating a [bug or feature request](https://github.com/istio/operator/blob/master/BUGS-AND-FEATURE-REQUESTS.md),
or just coming to the weekly [Environments Working Group](https://github.com/istio/community/blob/master/WORKING-GROUPS.md)
meeting to share your ideas. 

This document is an overview of how the operator works from a user perspective. For more details about the design and
architecture and a code overview, see [ARCHITECTURE.md](./ARCHITECTURE.md)

## Introduction

This repo reorganizes the current [Helm installation parameters](https://istio.io/docs/reference/config/installation-options/) into two groups:

- The new [platform level installation API](https://github.com/istio/operator/blob/master/pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto), for managing
K8s settings like resources, auto scaling, pod disruption budgets and others defined in the
[KubernetesResourceSpec](https://github.com/istio/operator/blob/905dd84e868a0b88c08d95b7ccf14d085d9a6f6b/pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto#L411)
- The configuration API that currently uses the
[Helm installation parameters](https://istio.io/docs/reference/config/installation-options/) for backwards
compatibility. This API is for managing the Istio control plane configuration settings.

Some parameters will temporarily exist in both APIs - for example, setting K8s resources currently can be done through
either API above. However, the Istio community recommends using the first API as it is more consistent, is validated,
and will naturally follow the graduation process for APIs while the same parameters in the configuration API are planned
for deprecation.

This repo currently provides pre-configured Helm values sets for different scenarios as configuration
[profiles](https://istio.io/docs/setup/kubernetes/additional-setup/config-profiles/), which act as a starting point for
an Istio install and can be customized by creating customization overlay files or passing parameters when
calling Helm. Similarly, the operator API uses the same profiles (expressed internally through the new API), which can be selected
as a starting point for the installation. For comparison, the following example shows the command needed to install
Istio using the SDS configuration profile using Helm:

```bash
helm template install/kubernetes/helm/istio --name istio --namespace istio-system \
    --values install/kubernetes/helm/istio/values-istio-sds-auth.yaml | kubectl apply -f -
```

In the new API, the same profile would be selected through a CustomResource (CR):

```yaml
# sds-install-cr.yaml

apiVersion: install.istio.io/v1alpha2
kind: IstioControlPlane
metadata:
  name: istio-operator-config
  namespace: istio-system
spec:
  profile: sds
```

CRs are used when running the operator as a controller in a pod in the cluster.
If you use the operator CLI mode and pass the configuration as a file, you only require the spec portion, for example:

```yaml
# sds-install.yaml

profile: sds
```

See [Select a profile](#select-a-profile) for more information.

If you don't specify a configuration profile, Istio is installed using the `default` configuration profile. All
profiles listed in istio.io are available compiled in, or `profile:` can point to a local file path to reference a custom
profile base to use as a starting point for customization. See the [API reference](https://github.com/istio/operator/blob/master/pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto)
for details.

## Developer quick start

The quick start describes how to install and use the operator `iop` CLI command.

### Installation

```bash
git clone https://github.com/istio/operator.git
cd operator
make iop
```

### Flags

The `iop` command supports the following flags:

- `logtostderr`: log to console (by default logs go to ./iop.go).
- `dry-run`: console output only, nothing applied to cluster or written to files (**default is true until the tool is robust enough**).
- `verbose`: display entire manifest contents and other debug info (default is false).

### Quick tour of CLI commands 

#### Basic default manifest

The following command generates a manifest with the compiled in default profile and charts:

```bash
iop manifest
```

You can see these sources for the compiled in profiles in the repo under `data/profiles`, while the compiled in Helm
charts are under `data/charts`.

#### Output to dirs

The output of the manifest is concatenated into a single file. To generate a directory hierarchy with subdirectory
levels representing a child dependency, use the following command:

```bash
iop manifest -o istio_manifests
```

(Note that the default dry-run setting only writes the intended actions to log.)
Use depth first search to traverse the created directory hierarchy when applying your YAML files. This is needed for
correct sequencing of dependencies. Child manifest directories must wait for their parent directory to be fully applied,
but not their sibling manifest directories.

#### Just apply it for me

The following command generates the manifests and applies them in the correct dependency order, waiting for the
dependencies to have the needed CRDs available:

```bash
iop install
```

The default `dry-run=true` displays the manifest only. Set `dry-run=false` to apply the Istio configuration to Kubernetes.
`--dry-run=false` to actually apply the generated configuration to the cluster.

#### Review the values of the current configuration profile

The following command shows the values of the current configuration profile:

```bash
iop dump-profile
```

#### Select a specific configuration profile

The simplest customization is to select a profile different to `default` e.g. `sds`. Create the following config file:

```yaml
# sds-install.yaml

profile: sds
```

Use the Istio operator `iop` binary to apply the new configuration profile:

```bash
iop manifest -f sds-install.yaml
```

After running the command, the Helm charts are rendered using `data/profiles/sds.yaml`.

#### Install from file path

The compiled in charts and profiles are used by default, but you can specify a file path, for example:

```yaml
profile: file:///usr/home/bob/go/src/github.com/ostromart/istio-installer/data/profiles/default.yaml
customPackagePath: file:///usr/home/bob/go/src/github.com/ostromart/istio-installer/data/charts/
```

You can mix and match these approaches. For example, you can use a compiled-in configuration profile with charts in your
local file system.

### New API customization

The [new platform level installation API](https://github.com/istio/operator/blob/95e89c4fb838b0b374f70d7c5814329e25a64819/pkg/apis/istio/v1alpha1/istioinstaller_types.proto#L25)
defines install time parameters like feature and component enablement and namespace, and K8s settings like resources, HPA spec etc. in a structured way.
The simplest customization is to turn features and components on and off. For example, to turn off all policy:

```yaml
profile: sds
policy:
  enabled: false
```

The operator validates the configuration and automatically detects syntax errors. Helm lacks this capability. If you are
using Helm values that are incompatible, the schema validation used in the operator may reject input that is valid for
Helm. Another customization is to define custom namespaces for features:

```yaml
profile: sds

trafficManagement:
  namespace: istio-control-custom
```

The traffic management feature comprises Pilot and Proxy components. Each of these components has K8s
settings, and these can be overridden from the defaults using official K8s APIs (rather than Istio defined schemas):

```yaml
trafficManagement:
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
```

The K8s settings are defined in detail in the
[operator API](https://github.com/istio/operator/blob/95e89c4fb838b0b374f70d7c5814329e25a64819/pkg/apis/istio/v1alpha1/istioinstaller_types.proto#L394).
The settings are the same for all components, so a user can configure pilot K8s settings in exactly the same, consistent
way as galley settings. Supported K8s settings currently include:

- [resources](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#resource-requests-and-limits-of-pod-and-container)
- [readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/)
- [replica count](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
- [HoriizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
- [PodDisruptionBudget](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#how-disruption-budgets-work)
- [pod annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [service annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
- [ImagePullPolicy](https://kubernetes.io/docs/concepts/containers/images/)
- [priority calss name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass)
- [node selector](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector)
- [affinity and anti-affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity)

All of these K8s settings use the K8s API definitions, so [K8s documentation](https://kubernetes.io/docs/concepts/) can
be used for reference. All K8s overlay values are also validated in the operator.

### Customizing the old values.yaml API

The new platform install API above deals with K8s level settings. The remaining values.yaml parameters deal with Istio
control plane operation rather than installation. For the time being, the operator just passes these through to the Helm
charts unmodified (but validated through a
[schema](https://github.com/istio/operator/blob/master/pkg/apis/istio/v1alpha2/values_types.go)). Values.yaml settings
are overridden the same way as the new API, though a customized CR overlaid over default values for the selected
profile. Here's an example of overriding some global level default values:

```yaml
profile: sds
values:
  logging:
    level: "default:warning" # override from info
```

Since from 1.3 Helm charts are split up per component, values overrides should be specified under the appropriate component e.g.

```yaml
trafficManagement:
  components:
    pilot:
      common:
        values:
          traceSampling: 0.1 # override from 1.0
```

### Advanced K8s resource overlays

Advanced users may occasionally have the need to customize parameters (like container command line flags) which are not
exposed through either of the installation or configuration APIs described in this document. For such cases, it's
possible to overlay the generated K8s resources before they are applied with user-defined overlays. For example, to
override some container level values in the Pilot container:

```yaml
trafficManagement:
  enabled: true
  components:
    proxy:
      common:
        enabled: false
    pilot:
      common:
        k8s:
          overlays:
          - kind: Deployment
            name: istio-pilot
            patches:
            - path: spec.template.spec.containers.[name:discovery].args.[30m]
              value: "60m" # OVERRIDDEN
            - path: spec.template.spec.containers.[name:discovery].ports.[containerPort:8080].containerPort
              value: 8090 # OVERRIDDEN
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

### Try the demo customization

This customization contains overlays at all three levels: the new API, values.yaml legacy API and the K8s output overlay.

```bash
iop manifest -f samples/customize_pilot.yaml
```

## Architecture

WIP, coming soon.
