# Istio

This chart bootstraps all istio [components](https://istio.io/docs/concepts/what-is-istio/overview.html) deployment on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Installing the Chart

```console
$ cd install/kubernetes/helm
$ helm install istio --name istio --namespace=istio-system
```

## Configuration
All configurable variables can be seen in `values.yaml`.

<!--- TODO:
 - describe all possible config options for the chart (values.yaml)
 --->
### Selecting components
This chart can install multiple istio components:
- proxy side card (by default)
- initializer
- mixer
- security (certificate authority)

To enable or disable then change the `enabled` flag of each component.

#### RBAC
If role-based access control (RBAC) is enabled in your cluster, you have two options:

1. Let the chart manage RBAC resources:
```console
$ helm install istio --name istio --namespace=istio-system --set=rbacEnabled=true
```
Also, if RBAC is enabled in your cluster, you may need to give [Tiller](https://docs.helm.sh/architecture/#components) additional permissions (`cluster-admin` cluster role for example). For more information follow this [instructions](cluster-admin).

2. Manage RBAC yourself. Create a service account with the desired permission and then pass this name to the chart by specifying the `serviceAccountName` for each component.

## Deleting the Chart

```console
$ helm delete istio --name istio --namespace=istio-system
```
