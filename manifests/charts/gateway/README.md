# Istio Gateway Helm Chart

This chart installs an Istio gateway deployment.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

To install the chart with the release name `istio-ingressgateway`:

```console
helm install istio-ingressgateway istio/gateway
```

## Uninstalling the Chart

To uninstall/delete the `istio-ingressgateway` deployment:

```console
helm delete istio-ingressgateway
```

## Configuration

To view supported configuration options and documentation, run:

```console
helm show values istio/gateway
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

### OpenShift

When deploying the gateway in an OpenShift cluster, use the `openshift` profile to override the default values, for example:

```console
helm install istio-ingressgateway istio/gateway --set profile=openshift
```

### `image: auto` Information

The image used by the chart, `auto`, may be unintuitive.
This exists because the pod spec will be automatically populated at runtime, using the same mechanism as [Sidecar Injection](istio.io/latest/docs/setup/additional-setup/sidecar-injection).
This allows the same configurations and lifecycle to apply to gateways as sidecars.

Note: this does mean that the namespace the gateway is deployed in must not have the `istio-injection=disabled` label.
See [Controlling the injection policy](https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#controlling-the-injection-policy) for more info.

### Examples

#### Egress Gateway

Deploying a Gateway to be used as an [Egress Gateway](https://istio.io/latest/docs/tasks/traffic-management/egress/egress-gateway/):

```yaml
service:
  # Egress gateways do not need an external LoadBalancer IP
  type: ClusterIP
```

#### Multi-network/VM Gateway

Deploying a Gateway to be used as a [Multi-network Gateway](https://istio.io/latest/docs/setup/install/multicluster/) for network `network-1`:

```yaml
networkGateway: network-1
```

### Migrating from other installation methods

Installations from other installation methods (such as istioctl, Istio Operator, other helm charts, etc) can be migrated to use the new Helm charts
following the guidance below.
If you are able to, a clean installation is simpler. However, this often requires an external IP migration which can be challenging.

WARNING: when installing over an existing deployment, the two deployments will be merged together by Helm, which may lead to unexpected results.

#### Legacy Gateway Helm charts

Istio historically offered two different charts - `manifests/charts/gateways/istio-ingress` and `manifests/charts/gateways/istio-egress`.
These are replaced by this chart.
While not required, it is recommended all new users use this chart, and existing users migrate when possible.

This chart has the following benefits and differences:
* Designed with Helm best practices in mind (standardized values options, values schema, values are not all nested under `gateways.istio-ingressgateway.*`, release name and namespace taken into account, etc).
* Utilizes Gateway injection, simplifying upgrades, allowing gateways to run in any namespace, and avoiding repeating config for sidecars and gateways.
* Published to official Istio Helm repository.
* Single chart for all gateways (Ingress, Egress, East West).

#### General concerns

For a smooth migration, the resource names and `Deployment.spec.selector` labels must match.

If you install with `helm install istio-gateway istio/gateway`, resources will be named `istio-gateway` and the `selector` labels set to:

```yaml
app: istio-gateway
istio: gateway # the release name with leading istio- prefix stripped
```

If your existing installation doesn't follow these names, you can override them. For example, if you have resources named `my-custom-gateway` with `selector` labels
`foo=bar,istio=ingressgateway`:

```yaml
name: my-custom-gateway # Override the name to match existing resources
labels:
  app: "" # Unset default app selector label
  istio: ingressgateway # override default istio selector label
  foo: bar # Add the existing custom selector label
```

#### Migrating an existing Helm release

An existing helm release can be `helm upgrade`d to this chart by using the same release name. For example, if a previous
installation was done like:

```console
helm install istio-ingress manifests/charts/gateways/istio-ingress -n istio-system
```

It could be upgraded with

```console
helm upgrade istio-ingress manifests/charts/gateway -n istio-system --set name=istio-ingressgateway --set labels.app=istio-ingressgateway --set labels.istio=ingressgateway
```

Note the name and labels are overridden to match the names of the existing installation.

Warning: the helm charts here default to using port 80 and 443, while the old charts used 8080 and 8443.
If you have AuthorizationPolicies that reference port these ports, you should update them during this process,
or customize the ports to match the old defaults.
See the [security advisory](https://istio.io/latest/news/security/istio-security-2021-002/) for more information.

#### Other migrations

If you see errors like `rendered manifests contain a resource that already exists` during installation, you may need to forcibly take ownership.

The script below can handle this for you. Replace `RELEASE` and `NAMESPACE` with the name and namespace of the release:

```console
KINDS=(service deployment)
RELEASE=istio-ingressgateway
NAMESPACE=istio-system
for KIND in "${KINDS[@]}"; do
    kubectl --namespace $NAMESPACE --overwrite=true annotate $KIND $RELEASE meta.helm.sh/release-name=$RELEASE
    kubectl --namespace $NAMESPACE --overwrite=true annotate $KIND $RELEASE meta.helm.sh/release-namespace=$NAMESPACE
    kubectl --namespace $NAMESPACE --overwrite=true label $KIND $RELEASE app.kubernetes.io/managed-by=Helm
done
```

You may ignore errors about resources not being found.
