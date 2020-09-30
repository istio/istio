# Helm v3 support

## Install

The Helm charts are supported both by Helm v2 and Helm v3. Please do not introduce Helm v3 specific changes as many
users are still using Helm v2 and the operator is currently using the Helm v2 code to generate.

To install with Helm v3, you must first create the namespace that you wish to install in if the namespace does not exist already. The default namespace used is `istio-system` and can be created as follows:

```console
kubectl create namespace istio-system
```

The charts are as follows:

- `base` creates cluster-wide CRDs, cluster bindings and cluster resources. It is possible to change the namespace from `istio-system` but it is not recommended.

```console
helm install istio-base -n istio-system manifests/charts/base
```

- `istio-control/istio-discovery` installs a revision of istiod.  You can install it multiple times, with different revisions.

```console
 helm install -n istio-system istio-17 manifests/charts/istio-control/istio-discovery

 helm install -n istio-system istio-canary manifests/charts/istio-control/istio-discovery \
    -f manifests/charts/global.yaml  --set revision=canary --set clusterResources=false

 helm install -n istio-system istio-mytest manifests/charts/istio-control/istio-discovery \
    -f manifests/charts/global.yaml  --set revision=mytest --set clusterResources=false
```

- `gateways` install a load balancer with `ingress` and `egress`. You can install it multiple times with different revisions but they must be installed in separate namespaces.

Ingress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-ingress manifests/charts/gateways/istio-ingress -f manifests/charts/global.yaml

kubectl create ns istio-ingress-canary
helm install -n istio-ingress-canary istio-ingress-canary manifests/charts/gateways/istio-ingress \
-f manifests/charts/global.yaml --set revision=canary
```

Egress secrets and access should be separated from the control plane.

```console
helm install -n istio-system istio-egress manifests/charts/gateways/istio-egress -f manifests/charts/global.yaml

kubectl create ns istio-egress-canary
helm install -n istio-egress-canary istio-egress-canary manifests/charts/gateways/istio-egress \
-f manifests/charts/global.yaml --set revision=canary
```

- 'istio-cni' installs the CNI plugin. This should be installed after the 'base' chart and prior to `istiod`. Need to add `--set istio_cni.enabled=true` to the `istiod` install to enable its usage.

```console
helm install istio-cni -n istio-system manifests/charts/istio-cni -f manifests/charts/global.yaml
```

## Namespaces

One of the changes in Helm v3 is that the namespace is no longer created on the fly when installing a chart. This means that the namespace being used needs to be created prior to installing the charts if it does not exist already. If the default `istio-system` namespace if not being used then you need to add the setting `--set global.istioNamespace=<namespace>` to the installs, to match the control plane namespace.
