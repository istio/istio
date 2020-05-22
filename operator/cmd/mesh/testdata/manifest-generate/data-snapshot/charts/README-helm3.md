# Helm3 support

## Install

The install templates support both helm2 and helm3. Please do not introduce helm3-specific changes, many
users are still using helm2 and the operator is currently using the helm2 code to generate.

We have few charts:

- 'base' creates cluster-wide CRDs, cluster bindings, cluster resources and the istio-system namespace.
  It is possible to customize the namespace, but not recommended.

```shell script
 helm3 install  istio-base manifests/charts/base
```

- 'istiod' installs a revision of istiod.  You can install it multiple times, with different revision.
TODO: get rid of global.yaml, anything still used should be in values.yaml for istio-discovery
TODO: remove the need to pass -n istio-system

```shell script
 helm3 install -n istio-system istio-16 manifests/charts/istio-control/istio-discovery \
    -f manifests/charts/global.yaml

 helm3 install -n istio-system istio-canary manifests/charts/istio-control/istio-discovery \
    -f manifests/charts/global.yaml  --set revision=canary --set clusterResources=false

 helm3 install -n istio-system istio-mytest manifests/charts/istio-control/istio-discovery \
    -f manifests/charts/global.yaml  --set revision=mytest --set clusterResources=false
```

- 'ingress' to install a Gateway

Helm3 requires namespaces to be created explicitly, currently we don't support installing multiple gateways in same
namespace - nor is it a good practice. Ingress secrets and access should be separated from control plane.

```shell script
    helm3 install -n istio-system istio-ingress manifests/charts/gateways/istio-ingress -f manifests/charts/global.yaml

    kubectl create ns istio-ingress-canary
    helm3 install -n istio-ingress-canary istio-ingress-canary manifests/charts/gateways/istio-ingress \
      -f manifests/charts/global.yaml --set revision=canary
```

## Namespaces

One of the major changes in helm3 is that the 'release namespace' is no longer created.
That means the first step can't use "-n istio-system" flag, instead we use .global.istioNamespace.
It is possible - but not supported - to install multiple versions of the global, for example in
multi-tenant cases. Each namespace will have a separate root CA, if the built-in CA is used.

TODO: apply the same change for discovery and ingress, so passing -n is no longer needed.

