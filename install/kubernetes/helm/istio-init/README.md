# Istio

[Istio](https://istio.io/) is an open platform for providing a uniform way to integrate microservices, manage traffic flow across microservices, enforce policies and aggregate telemetry data.

## Introduction

This chart bootstraps Istio's [CRDs](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#customresourcedefinitions)
which are an internal implementation detail of Istio.  CRDs define data structures for storing runtime configuration
specified by a human operator.

This chart must be run to completion prior to running other Istio charts, or other Istio charts will fail to initialize.

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.7.2 or newer or alternately the ability to modify RBAC rules is also required

## Resources Required

The chart deploys pods that consume minimal resources.

## Installing the Chart

1. If a service account has not already been installed for Tiller, install one:
    ```
    $ kubectl apply -f install/kubernetes/helm/helm-service-account.yaml
    ```

1. If Tiller has not already been installed in your cluster, Install Tiller on your cluster with the service account:
    ```
    $ helm init --service-account tiller
    ```

1. Install the Istio initializer chart:
    ```
    $ helm install install/kubernetes/helm/istio-init --name istio-init --namespace istio-system
    ```

    > Although you can install the `istio-init` chart to any namespace, it is recommended to install `istio-init` in the same namespace(`istio-system`) as other Istio charts.

## Configuration

The Helm chart ships with reasonable defaults.  There may be circumstances in which defaults require overrides.
To override Helm values, use `--set key=value` argument during the `helm install` command.  Multiple `--set` operations may be used in the same Helm operation.

Helm charts expose configuration options which are currently in alpha.  The currently exposed options are explained in the following table:

| Parameter | Description | Values | Default |
| --- | --- | --- | --- |
| `global.hub` | Specifies the HUB for most images used by Istio | registry/namespace | `docker.io/istio` |
| `global.tag` | Specifies the TAG for most images used by Istio | valid image tag | `0.8.latest` |
| `global.imagePullPolicy` | Specifies the image pull policy | valid image pull policy | `IfNotPresent` |


## Uninstalling the Chart

> Uninstalling this chart does not delete Istio's registered CRDs.  Istio by design expects
> CRDs to leak into the Kubernetes environment.  As CRDs contain all runtime configuration
> data in CustomResources the Istio designers feel it is better to explicitly delete this
> configuration rather then unexpectedly lose it.

To uninstall/delete the `istio-init` release but continue to track the release:
    ```
    $ helm delete istio-init
    ```

To uninstall/delete the `istio-init` release completely and make its name free for later use:
    ```
    $ helm delete istio-init --purge
    ```

> Warning: Deleting CRDs will delete any configuration that you have made to Istio.

To delete all CRDs, run the following command
    ```
    $ for i in istio-init/files/*crd*yaml; do kubectl delete -f $i; done
    ```
