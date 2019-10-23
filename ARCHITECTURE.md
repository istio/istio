# Istio operator code overview

## Introduction

This document covers primarily the code, with some background on how the design maps to it.
See the 
[design doc](https://docs.google.com/document/d/11j9ZtYWNWnxQYnZy8ayZav1FMwTH6F6z6fkDYZ7V298/edit#heading=h.qex63c29z2to)
for a more complete design description. The operator code is divided roughly into five areas:

1. [IstioControlPlaneSpec API](#istiocontrolplanespec-api) and related infrastructure, which is expressed as a
[proto](pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto) and
compiled to [Go
structs](pkg/apis/istio/v1alpha2/istiocontrolplane_types.pb.go).
`IstioControlPlaneSpec` has pass-through fields to the Helm values.yaml API, but these are additionally validated through
a [schema](pkg/apis/istio/v1alpha2/values/values_types.proto).
1. [Controller](#k8s-controller) code. The code comprises the K8s listener, webhook and logic for reconciling the cluster
to an `IstioControlPlaneSpec` CR. 
1. [Manifest creation](#manifest-creation) code. User settings are overlaid on top of the
selected profile values and passed to a renderer in the Helm library to create manifests. Further customization on the
created manifests can be done through overlays. 
1. [CLI](#cli) code. CLI code shares the `IstioControlPlaneSpec` API with
the controller, but allows manifests to be generated and optionally applied from the command line without the need to
run a privileged controller in the cluster. 
1. [Migration tools](#migration-tools). The migration tools are intended to
automate configuration migration from Helm to the operator.

The operator code uses the new Helm charts in the [istio/installer](https://github.com/istio/installer) repo. It is not
compatible with the older charts in [istio/istio](https://github.com/istio/istio/tree/master/install/kubernetes/helm).
See the istio/installer repo for details about the new charts and why they were created. Briefly, the new charts
are intended to support production ready deployments of Istio that follow best practices like canarying for upgrade.

## Terminology

Throughout the document, the following terms are used:

- `IstioControlPlaneSpec`: The API directly defined in the
[IstioControlPlaneSpec proto](pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto),
including feature and component groupings, namespaces and enablement, and per-component K8s settings. 
- Helm values.yaml API, implicitly defined through the various values.yaml files in the
[Helm charts](https://github.com/istio/installer) and schematized in the operator through
[values_types.proto](pkg/apis/istio/v1alpha2/values/values_types.proto).

## IstioControlPlaneSpec API

The `IstioControlPlaneSpec` API is intended to replace the installation and K8s parts of Helm values.yaml.

### Features and components

The operator has a very similar structure to istio/installer: components are grouped into features.
`IstioControlPlaneSpec` defines functional settings at the feature level. Functional settings are those that performs some
function in the Istio control plane without necessarily being tied to any one component that runs in a Deployment.
Component settings are those that necessarily refer to a particular Deployment or Service. For example, the number
of Pilot replicas is a component setting, because it refers to a component which is a Deployment in the
cluster. Most K8s platform settings are necessarily component settings.
The available features and the components that comprise each feature are as follows:

| Feature | Components |
|---------|------------|
Base | CRDs
Traffic Management | Pilot
Policy | Policy
Telemetry | Telemetry
Security | Citadel
Security | Node agent
Security | Cert manager
Configuration management | Galley
Gateways | Ingress gateway
Gateways | Egress gateway
AutoInjection | Sidecar injector

Features and components are defined in the
[name](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/name/name.go#L44) package.

Note: Besides the features and the components listed in the table above, some addon features and components are as follows:

| Feature | Components |
|---------|------------|
Telemetry | Prometheus
Telemetry | Prometheus Operator
Telemetry | Grafana
Telemetry | Kiali
Telemetry | Tracing
ThirdParty | CNI

### Namespaces

The `IstioControlPlaneSpec` API and underlying new Helm charts offer a lot of flexibility in which namespaces features and
components are installed into. Namespace definitions can be defined and specialized at the global, feature and component
level, with each lower level overriding the setting of the higher parent level. For example, if the global default
namespace is defined as:

```yaml
defaultNamespace: istio-system
```

and namespaces are specialized for the security feature and one of the components:

```yaml
security:
  components:
    namespace: istio-security
    citadel:
    nodeAgent:
      namespace: istio-security-nodeagent
policy:
  components:
    policy:
```

the resulting namespaces will be:

| Component | Namespace |
| --------- | :-------- |
policy | istio-system
citadel | istio-security
nodeAgent | istio-security-nodeagent

These rules are expressed in code in the
[name](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/name/name.go#L246) package.

### Enablement

Features and components can be individually or collectively enabled or disabled. If a feature is disabled, all of its
components are disabled, regardless of their component-level enablement. If a feature is enabled, all of its components
are enabled, unless they are individually disabled. For example:

```yaml
security:
  enabled: true
  components:
    citadel:
      enabled: false
```

will enable all components of the security feature except citadel.

These rules are expressed in code in the
[name](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/name/name.go#L131) package.

### K8s settings

Rather than defining selective mappings from parameters to fields in K8s resources, the `IstioControlPlaneSpec` API
contains a consistent K8s block for each Istio component. The available K8s settings are defined in
[KubernetesResourcesSpec](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/apis/istio/v1alpha2/istiocontrolplane_types.proto#L395):

| Field name | K8s API reference |
| :--------- | :---------------- |
resources | [resources](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#resource-requests-and-limits-of-pod-and-container)
readinessProbe | [readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-probes/)
replicaCount | [replica count](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/)
hpaSpec | [HorizontalPodAutoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
podDisruptionBudget | [PodDisruptionBudget](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#how-disruption-budgets-work)
podAnnotations | [pod annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
env | [container environment variables](https://kubernetes.io/docs/tasks/inject-data-application/define-environment-variable-container/)
imagePullPolicy| [ImagePullPolicy](https://kubernetes.io/docs/concepts/containers/images/)
priorityClassName | [priority class name](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass)
nodeSelector| [node selector](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector)
affinity | [affinity and anti-affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity)

These K8s setting are available for each component under the `k8s` field, for example:

```yaml
trafficManagement:
  components:
    pilot:
      k8s:
        hpaSpec:
          # HPA spec, as defined in K8s API
```

### Translations

API translations are version specific and are expressed as a
[table of Translators](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L110)
indexed by minor [version](pkg/version/version.go). This is because
mapping rules are only allowed to change between minor (not patch) versions.

The `IstioControlPlaneSpec` API fields are translated to the output manifest in two ways:

1. The `IstioControlPlaneSpec` API fields are mapped to the Helm values.yaml schema using the
[APIMapping](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L112)
field of the [Translator](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L52)
struct.
1. The K8s settings are applied to resources in the output manifest using the
[KubernetesMapping](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L132)
field in the [Translator](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L52)
struct.

Other per-component mappings to Helm values.yaml are expressed in the
[ComponentMaps](https://github.com/istio/operator/blob/e9097258cb4fbe59648e7da663cdad6f16927b8f/pkg/translate/translate.go#L83)
struct.

### Validations

Both the `IstioControlPlaneSpec` and Helm APIs are validated. The `IstioControlPlaneSpec` API is validated through a 
table of validation rules in
[pkg/validate/validate.go](pkg/validate/validate.go). These rules
refer to the Go struct path schema and hence have names with a capitalized first letter.
The Helm values.yaml API is validated in
[validate_values.go](pkg/validate/validate_values.go)
and refer to the values.yaml data paths. Hence, these rules have names with a lower case first letter.
Apart from validating the correctness of individual fields, the operator ensure that relationships between values in
different parts of the configuration tree are correct. For example, it's an error to enable a component while its
parent feature is disabled.

## K8s controller

TODO(rcernich).

## Manifest creation

Manifest rendering is a multi-step process, shown in the figure below. ![rendering
process](images/operator_render_flow.svg) The example in the figure shows the rendering being triggered by a CLI `mesh`
command with a `IstioControlPlaneSpec` CR passed to it from a file; however, the same rendering steps would occur when an
in-cluster CR is updated and the controller acts upon it to generate a new manifest to apply to the cluster. Note that
both the charts and configuration profiles can come from three different sources: compiled-in, local filesystem, or URL
(TODO(mostrowski): describe the remote URL functionality). 
The source may be selected independently for the charts and profiles. The different steps in creating the manifest are
as follows:

1. The user CR (my_custom.yaml) selects a configuration profile. If no profile is selected, the 
[default profile](data/profiles/default.yaml) is used. Each profile is defined as a
set of defaults for `IstioControlPlaneSpec`, for both the restructured fields (K8s settings, namespaces and enablement)
and the Helm values (Istio behavior configuration).

1. The fields defined in the user CR override any values defined in the configuration profile CR.  The
resulting CR is converted to Helm values.yaml format and passed to the next step.
1. Part of the configuration profile contains settings in the Helm values.yaml schema format. User overrides of
these fields are applied and merged with the output of this step. The result of this step is a merge of configuration
profile defaults and user overlays, all expressed in Helm values.yaml format. This final values.yaml configuration
is passed to the Helm rendering library and used to render the charts. The rendered manifests are passed to the next
step.
1. Overlays in the user CR are applied to the rendered manifests. No values are ever defined in configuration profile
CRs at this layer, so no merge is performed in this step.

## CLI

The CLI `mesh` command is implemented in the [cmd/mesh](cmd/mesh/)
subdirectory as a Cobra command with the following subcommands:

- [manifest](cmd/mesh/manifest.go): the manifest subcommand is used to generate, apply, diff or migrate Istio manifests, it has the following subcommands:
    - [apply](cmd/mesh/manifest-apply.go): the apply subcommand is used to generate an Istio install manifest and apply it to a cluster.
    - [diff](cmd/mesh/manifest-diff.go): the diff subcommand is used to compare manifest from two files or directories.
    - [generate](cmd/mesh/manifest-generate.go): the generate subcommand is used to generate an Istio install manifest.
    - [migrate](cmd/mesh/manifest-migrate.go): the migrate subcommand is used to migrate a configuration in Helm values format to IstioControlPlane format.
    - [versions](cmd/mesh/manifest-versions.go): the versions subcommand is used to list the version of Istio recommended for and supported by this version of the operator binary.
- [profile](cmd/mesh/profile.go): dumps the default values for a selected profile, it has the following subcommands:
    - [diff](cmd/mesh/profile-diff.go): the diff subcommand is used to display the difference between two Istio configuration profiles.
    - [dump](cmd/mesh/profile-dump.go): the dump subcommand is used to dump the values in an Istio configuration profile.
    - [list](cmd/mesh/profile-list.go): the list subcommand is used to list available Istio configuration profiles.
- [upgrade](cmd/mesh/upgrade.go): performs an in-place upgrade of the Istio control plane with eligibility checks.

## Migration tools

TODO(richardwxn).
