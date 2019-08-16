# Istio

[Istio](https://istio.io/) is an open platform for providing a uniform way to integrate microservices, manage traffic flow across microservices, enforce policies and aggregate telemetry data.



The documentation here is for developers only, please follow the installation instructions from [istio.io](https://istio.io/docs/setup/kubernetes/install/helm/) for all other uses.

## Introduction

This chart bootstraps all Istio [components](https://istio.io/docs/concepts/what-is-istio/) deployment on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Chart Details

This chart can install multiple Istio components as subcharts:
- ingressgateway
- egressgateway
- sidecarInjectorWebhook
- galley
- mixer
- pilot
- security(citadel)
- grafana
- prometheus
- tracing(jaeger)
- kiali

To enable or disable each component, change the corresponding `enabled` flag.

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.7.2 or newer or alternately the ability to modify RBAC rules is also required
- If you want to enable automatic sidecar injection, Kubernetes 1.9+ with `admissionregistration` API is required, and `kube-apiserver` process must have the `admission-control` flag set with the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook` admission controllers added and listed in the correct order.
- The `istio-init` chart must be run to completion prior to install the `istio` chart.

## Resources Required

The chart deploys pods that consume minimum resources as specified in the resources configuration parameter.

## Installing the Chart

1. If a service account has not already been installed for Tiller, install one:
    ```
    $ kubectl apply -f install/kubernetes/helm/helm-service-account.yaml
    ```

1. Install Tiller on your cluster with the service account:
    ```
    $ helm init --service-account tiller
    ```

1. Set and create the namespace where Istio was installed:
    ```
    $ NAMESPACE=istio-system
    $ kubectl create ns $NAMESPACE
    ```

1. If you are enabling `kiali`, you need to create the secret that contains the username and passphrase for `kiali` dashboard:
    ```
    $ echo -n 'admin' | base64
    YWRtaW4=
    $ echo -n '1f2d1e2e67df' | base64
    MWYyZDFlMmU2N2Rm
    $ cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
      name: kiali
      namespace: $NAMESPACE
      labels:
        app: kiali
    type: Opaque
    data:
      username: YWRtaW4=
      passphrase: MWYyZDFlMmU2N2Rm
    EOF
    ```

1. If you are using security mode for Grafana, create the secret first as follows:

    - Encode username, you can change the username to the name as you want:
    ```
    $ echo -n 'admin' | base64
    YWRtaW4=
    ```

    - Encode passphrase, you can change the passphrase to the passphrase as you want:
    ```
    $ echo -n '1f2d1e2e67df' | base64
    MWYyZDFlMmU2N2Rm
    ```

    - Create secret for Grafana:
    ```
    $ cat <<EOF | kubectl apply -f -
    apiVersion: v1
    kind: Secret
    metadata:
      name: grafana
      namespace: $NAMESPACE
      labels:
        app: grafana
    type: Opaque
    data:
      username: YWRtaW4=
      passphrase: MWYyZDFlMmU2N2Rm
    EOF
    ```

1. To install the chart with the release name `istio` in namespace $NAMESPACE you defined above:

    - With [automatic sidecar injection](https://istio.io/docs/setup/kubernetes/sidecar-injection/#automatic-sidecar-injection) (requires Kubernetes >=1.9.0):
    ```
    $ helm install istio --name istio --namespace $NAMESPACE
    ```

    - Without the sidecar injection webhook:
    ```
    $ helm install istio --name istio --namespace $NAMESPACE --set sidecarInjectorWebhook.enabled=false
    ```

## Configuration

The Helm chart ships with reasonable defaults.  There may be circumstances in which defaults require overrides.
To override Helm values, use `--set key=value` argument during the `helm install` command.  Multiple `--set` operations may be used in the same Helm operation.

Helm charts expose configuration options which are currently in alpha.  The currently exposed options can be found [here](https://istio.io/docs/reference/config/installation-options/).

## Uninstalling the Chart

To uninstall/delete the `istio` release but continue to track the release:
    ```
    $ helm delete istio
    ```

To uninstall/delete the `istio` release completely and make its name free for later use:
    ```
    $ helm delete --purge istio
    ```
