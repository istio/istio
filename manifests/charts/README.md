# Istio Installer

Note: If making any changes to the charts or values.yaml in this dir, first read [UPDATING-CHARTS.md](UPDATING-CHARTS.md)

Istio installer is a modular, 'a-la-carte' installer for Istio. It is based on a
fork of the Istio helm templates, refactored to increase modularity and isolation.

Goals:
- Improve upgrade experience: users should be able to gradually roll upgrades, with proper
canary deployments for Istio components. It should be possible to deploy a new version while keeping the
stable version in place and gradually migrate apps to the new version.

- More flexibility: the new installer allows multiple 'environments', allowing applications to select
a set of control plane settings and components. While the entire mesh respects the same APIs and config,
apps may target different 'environments' which contain different instances and variants of Istio.

- Better security: separate Istio components reside in different namespaces, allowing different teams or
roles to manage different parts of Istio. For example, a security team would maintain the
root CA and policy, a telemetry team may only have access to Prometheus,
and a different team may maintain the control plane components (which are highly security sensitive).

The install is organized in 'environments' - each environment consists of a set of components
in different namespaces that are configured to work together. Regardless of 'environment',
workloads can talk with each other and obey the Istio configuration resources, but each environment
can use different Istio versions and different configuration defaults.

`istioctl kube-inject` or the automatic sidecar injector are used to select the environment.
In the case of the sidecar injector, the namespace label `istio-env: <NAME_OF_ENV>` is used instead
of the conventional `istio-injected: true`. The name of the environment is defined as the namespace
where the corresponding control plane components (config, discovery, auto-injection) are running.
In the examples below, by default this is the `istio-control` namespace. Pod annotations can also
be used to select a different 'environment'.

## Installing

The new installer is intended to be modular and very explicit about what is installed. It has
far more steps than the Istio installer - but each step is smaller and focused on a specific
feature, and can be performed by different people/teams at different times.

It is strongly recommended that different namespaces are used, with different service accounts.
In particular access to the security-critical production components (root CA, policy, control)
should be locked down and restricted.  The new installer allows multiple instances of
policy/control/telemetry - so testing/staging of new settings and versions can be performed
by a different role than the prod version.

The intended users of this repo are users running Istio in production who want to select, tune
and understand each binary that gets deployed, and select which combination to use.

Note: each component can be installed in parallel with an existing Istio 1.0 or 1.1 install in
`istio-system`. The new components will not interfere with existing apps, but can interoperate
and it is possible to gradually move apps from Istio 1.0/1.1 to the new environments and
across environments ( for example canary -> prod )

Note: there are still some cluster roles that may need to be fixed, most likely cluster permissions
will need to move to the security component.

## Everything is Optional

Each component in the new installer is optional. Users can install the component defined in the new installer,
use the equivalent component in `istio-system`, configured with the official installer, or use a different
version or implementation.

For example you may use your own Prometheus and Grafana installs, or you may use a specialized/custom
certificate provisioning tool, or use components that are centrally managed and running in a different cluster.

This is a work in progress - building on top of the multi-cluster installer.

As an extreme, the goal is to be possible to run Istio workloads in a cluster without installing any Istio component
in that cluster. Currently the minimum we require is the security provider (node agent or citadel).

### Install Istio CRDs

This is the first step of the install. Please do not remove or edit any CRD - config currently requires
all CRDs to be present. On each upgrade it is recommended to reapply the file, to make sure
you get all CRDs.  CRDs are separated by release and by component type in the CRD directory.

Istio has strong integration with certmanager.  Some operators may want to keep their current certmanager
CRDs in place and not have Istio modify them.  In this case, it is necessary to apply CRD files individually.

```bash
kubectl apply -k github.com/istio/installer/base
```

or

```bash
kubectl apply -f base/files
```

### Install Istio-CNI

This is an optional step - CNI must run in a dedicated namespace, it is a 'singleton' and extremely
security sensitive. Access to the CNI namespace must be highly restricted.

**NOTE:** The environment variable `ISTIO_CLUSTER_ISGKE` is assumed to be set to `true` if the cluster
is a GKE cluster.

```bash
ISTIO_CNI_ARGS=
# TODO: What k8s data can we use for this check for whether GKE?
if [[ "${ISTIO_CLUSTER_ISGKE}" == "true" ]]; then
    ISTIO_CNI_ARGS="--set cni.cniBinDir=/home/kubernetes/bin"
fi
iop kube-system istio-cni $IBASE/istio-cni/ ${ISTIO_CNI_ARGS}
```

TODO. It is possible to add Istio-CNI later, and gradually migrate.

### Install Control plane

This can run in any cluster. A mesh should have at least one cluster should run Pilot or equivalent XDS server,
and it is recommended to have Pilot running in each region and in multiple availability zones for multi cluster.

```bash
iop istio-control istio-discovery $IBASE/istio-control/istio-discovery \
            --set global.istioNamespace=istio-system

# Second istio-discovery, using master version of istio
TAG=latest HUB=gcr.io/istio-testing iop istio-master istio-discovery-master $IBASE/istio-control/istio-discovery \
            --set policy.enable=false \
            --set global.istioNamespace=istio-master
```

### Gateways

A cluster may use multiple Gateways, each with a different load balancer IP, domains and certificates.

Since the domain certificates are stored in the gateway namespace, it is recommended to keep each
gateway in a dedicated namespace and restrict access.

For large-scale gateways it is optionally possible to use a dedicated pilot in the gateway namespace.

### Additional test templates

A number of helm test setups are general-purpose and should be installable in any cluster, to confirm
Istio works properly and allow testing the specific install.
