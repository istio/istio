# Istio

[Istio](https://istio.io/) is an open platform for providing a uniform way to integrate microservices, manage traffic flow across microservices, enforce policies and aggregate telemetry data.

## Introduction

This chart is used to installed on remote cluster to connect with istio control plane on a local cluster.

## Chart Details

This chart will install security(citadel) component and register three services on a remote cluster:
- istio-pilot
- istio-policy
- istio-statsd-prom-bridge

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.7.2 or newer or alternately the ability to modify RBAC rules is also required
- Set environment variables for Pod IPs from Istio control plane needed by remote:
```
export PILOT_POD_IP=$(kubectl -n istio-system get pod -l istio=pilot -o jsonpath='{.items[0].status.podIP}')
export POLICY_POD_IP=$(kubectl -n istio-system get pod -l istio=mixer -o jsonpath='{.items[0].status.podIP}')
export STATSD_POD_IP=$(kubectl -n istio-system get pod -l istio=statsd-prom-bridge -o jsonpath='{.items[0].status.podIP}')
```

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

3. To install the chart with the release name `istio-remote` in namespace `istio-system`:
```
$ helm install install/kubernetes/helm/istio-remote --name istio-remote --set global.pilotEndpoint=${PILOT_POD_IP} --set global.policyEndpoint=${POLICY_POD_IP} --set global.statsdEndpoint=${STATSD_POD_IP} --namespace istio-system
```

## Configuration

The `isito-remote` Helm chart requires the three specific variables to be configured as defined in the following table:

| Helm Variable | Accepted Values | Default | Purpose of Value |
| --- | --- | --- | --- |
| `global.pilotEndpoint` | A valid IP address | istio-pilot.istio-system | Specifies the Istio control plane's pilot Pod IP address |
| `global.policyEndpoint` | A valid IP address | istio-policy.istio-system | Specifies the Istio control plane's policy Pod IP address |
| `global.statsdEndpoint` | A valid IP address | istio-statsd-prom-bridge.istio-system | Specifies the Istio control plane's statsd Pod IP address |

## Uninstalling the Chart

To uninstall/delete the `istio-remote` release:
```
$ helm delete istio-remote
```
The command removes all the Kubernetes components associated with the chart and deletes the release.

To uninstall/delete the `istio-remote` release completely and make its name free for later use:
```
$ helm delete istio-remote --purge
```
