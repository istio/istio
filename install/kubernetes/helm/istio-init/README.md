# Istio

[Istio](https://istio.io/) is an open platform for providing a uniform way to integrate microservices, manage traffic flow across microservices, enforce policies and aggregate telemetry data.

## Introduction

This chart bootstraps [CRDs](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#customresourcedefinitions)
which are an internal implementation detail of Istio.

This chart must be run to completion prior to running other Istio charts, or other Istio charts will fail to initialize.

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.7.2 or newer or alternately the ability to modify RBAC rules is also required

## Resources Required

The chart deploys pods that consume minimum resources as specified in the resources configuration parameter.

## Installing the Chart

1. If a service account has not already been installed for Tiller, install one:
    ```
    $ kubectl apply -f install/kubernetes/helm/helm-service-account.yaml
    ```

1. If Tiller has not already been installed in your cluster, Install Tiller on your cluster with the service account:
    ```
    $ helm init --service-account tiller
    ```

1. Install the Istio initalizer chart:
    ```
    $ helm install install/kubernetes/helm/istio-init --name istio-init
    ```

## Configuration

This helm chart has no configuration options.

## Uninstalling the Chart

To uninstall/delete the `istio` release but continue to track the release:
    ```
    $ helm delete istio-init
    ```

To uninstall/delete the `istio` release completely and make its name free for later use:
    ```
    $ helm delete istio-init --purge
    ```
