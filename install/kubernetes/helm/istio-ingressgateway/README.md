# Istio Ingress Gateway

[Gateway](https://istio.io/docs/reference/config/istio.networking.v1alpha3/#Gateway) describes a load balancer operating at the edge of the mesh receiving incoming or outgoing HTTP/TCP connections.  

## Introduction

This chart allows user to install a standalone [Istio ingress gateway](https://istio.io/docs/tasks/traffic-management/ingress/). 

## Chart Details

This chart can install multiple istio components as subcharts:
- ingressgateway
- certmanager

To enable or disable each component, change the corresponding `enabled` flag.

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.9.1 or newer or alternately the ability to modify RBAC rules is also required

## Resources Required

The chart deploys pods that consume minimum resources as specified in the resources configuration parameter.

## Installing the Chart

1. If a service account has not already been installed for Tiller, install one:
```
$ kubectl apply -f install/kubernetes/helm/helm-service-account.yaml
```

2. Install Tiller on your cluster with the service account:
```
$ helm init --service-account tiller
```

3. To install the chart with the release name `istio` in namespace `istio-system`:

    ```
    $ helm install install/kubernetes/helm/istio-ingressgateway --name istio-ingress-gateway --namespace kube-system
    ```

## Configuration

The Helm chart ships with reasonable defaults.  There may be circumstances in which defaults require overrides.
To override Helm values, use `--set key=value` argument during the `helm install` command.  Multiple `--set` operations may be used in the same Helm operation.

Helm charts expose configuration options which are currently in alpha.  The currently exposed options are explained in the following table:

| Parameter | Description | Values | Default |
| --- | --- | --- | --- |
| `global.hub` | Specifies the HUB for most images used by Istio | registry/namespace | `docker.io/istio` |
| `global.tag` | Specifies the TAG for most images used by Istio | valid image tag | `0.8.latest` |
| `global.proxy.image` | Specifies the proxy image name | valid proxy name | `proxyv2` |
| `global.imagePullPolicy` | Specifies the image pull policy | valid image pull policy | `IfNotPresent` |
| `global.arch.amd64` | Specifies the scheduling policy for `amd64` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `global.arch.s390x` | Specifies the scheduling policy for `s390x` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `global.arch.ppc64le` | Specifies the scheduling policy for `ppc64le` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `gateways.istio-ingressgateway.enabled` | Specifies whether Ingress gateway should be installed | true/false | `true` |

## Uninstalling the Chart

To uninstall/delete the `istio-ingressgateway` release:
```
$ helm delete istio-ingressgateway
```
The command removes all the Kubernetes components associated with the chart and deletes the release.

To uninstall/delete the `istio-ingressgateway` release completely and make its name free for later use:
```
$ helm delete istio-ingressgateway --purge
```
