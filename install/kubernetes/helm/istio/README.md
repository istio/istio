# Istio

[Istio](https://istio.io/) is an open platform for providing a uniform way to integrate microservices, manage traffic flow across microservices, enforce policies and aggregate telemetry data.

## Introduction

This chart bootstraps all istio [components](https://istio.io/docs/concepts/what-is-istio/overview.html) deployment on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Chart Details

This chart can install multiple istio components as subcharts:
- ingress
- ingressgateway
- egressgateway
- sidecarInjectorWebhook
- galley
- mixer
- pilot
- security(citadel)
- grafana
- prometheus
- servicegraph
- tracing(jaeger)
- kiali

To enable or disable each component, change the corresponding `enabled` flag.

## Prerequisites

- Kubernetes 1.9 or newer cluster with RBAC (Role-Based Access Control) enabled is required
- Helm 2.7.2 or newer or alternately the ability to modify RBAC rules is also required
- If you want to enable automatic sidecar injection, Kubernetes 1.9+ with `admissionregistration` API is required, and `kube-apiserver` process must have the `admission-control` flag set with the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook` admission controllers added and listed in the correct order.

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

3. Install Istio’s [Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#customresourcedefinitions) via `kubectl apply`, and wait a few seconds for the CRDs to be committed in the kube-apiserver:
   ```
   $ kubectl apply -f install/kubernetes/helm/istio/templates/crds.yaml
   ```
   **Note**: If you are enabling `certmanager`, you also need to install its CRDs and wait a few seconds for the CRDs to be committed in the kube-apiserver:
   ```
   $ kubectl apply -f install/kubernetes/helm/istio/charts/certmanager/templates/crds.yaml
   ```

4. If you are enabling `kiali`, you need to create the secret that contains the username and passphrase for `kiali` dashboard:
   ```
   $ echo -n 'admin' | base64
   YWRtaW4=
   $ echo -n '1f2d1e2e67df' | base64
   MWYyZDFlMmU2N2Rm
   $ NAMESPACE=istio-system
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

5. If you are using security mode for Grafana, create the secret first as follows:

Encode username, you can chage the username to the name as you want:
```
$ echo -n 'admin' | base64
YWRtaW4=
```

Encode passphrase, you can chage the passphrase to the passphrase as you want:
```
$ echo -n '1f2d1e2e67df' | base64
MWYyZDFlMmU2N2Rm
```

Set the namespace where Istio was installed:
```
$ NAMESPACE=istio-system
```

Create secret for Grafana:
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

6. To install the chart with the release name `istio` in namespace `istio-system`:
    - With [automatic sidecar injection](https://istio.io/docs/setup/kubernetes/sidecar-injection/#automatic-sidecar-injection) (requires Kubernetes >=1.9.0):
    ```
    $ helm install install/kubernetes/helm/istio --name istio --namespace istio-system
    ```

    - Without the sidecar injection webhook:
    ```
    $ helm install install/kubernetes/helm/istio --name istio --namespace istio-system --set sidecarInjectorWebhook.enabled=false
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
| `global.proxy.concurrency` | Specifies the number of proxy worker threads | number, 0 = auto | `0` |
| `global.imagePullPolicy` | Specifies the image pull policy | valid image pull policy | `IfNotPresent` |
| `global.controlPlaneSecurityEnabled` | Specifies whether control plane mTLS is enabled | true/false | `false` |
| `global.mtls.enabled` | Specifies whether mTLS is enabled by default between services | true/false | `false` |
| `global.rbacEnabled` | Specifies whether to create Istio RBAC rules or not | true/false | `true` |
| `global.refreshInterval` | Specifies the mesh discovery refresh interval | integer followed by s | `10s` |
| `global.arch.amd64` | Specifies the scheduling policy for `amd64` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `global.arch.s390x` | Specifies the scheduling policy for `s390x` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `global.arch.ppc64le` | Specifies the scheduling policy for `ppc64le` architectures | 0 = never, 1 = least preferred, 2 = no preference, 3 = most preferred | `2` |
| `ingress.enabled` | Specifies whether Ingress should be installed | true/false | `true` |
| `gateways.istio-ingressgateway.enabled` | Specifies whether Ingress gateway should be installed | true/false | `true` |
| `gateways.istio-egressgateway.enabled` | Specifies whether Egress gateway should be installed | true/false | `true` |
| `sidecarInjectorWebhook.enabled` | Specifies whether automatic sidecar-injector should be installed | `true` |
| `galley.enabled` | Specifies whether Galley should be installed for server-side config validation | true/false | `true` |
| `mixer.enabled` | Specifies whether Mixer should be installed | true/false | `true` |
| `pilot.enabled` | Specifies whether Pilot should be installed | true/false | `true` |
| `grafana.enabled` | Specifies whether Grafana addon should be installed | true/false | `false` |
| `grafana.persist` | Specifies whether Grafana addon should persist config data | true/false | `false` |
| `grafana.storageClassName` | If `grafana.persist` is true, specifies the [`StorageClass`](https://kubernetes.io/docs/concepts/storage/storage-classes/) to use for the `PersistentVolumeClaim` | `StorageClass` | "" |
| `grafana.accessMode` | If `grafana.persist` is true, specifies the [`Access Mode`](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes) to use for the `PersistentVolumeClaim` | RWO/ROX/RWX | `ReadWriteMany` |
| `prometheus.enabled` | Specifies whether Prometheus addon should be installed | true/false | `true` |
| `servicegraph.enabled` | Specifies whether Servicegraph addon should be installed | true/false | `false` |
| `tracing.enabled` | Specifies whether Tracing(jaeger) addon should be installed | true/false | `false` |
| `kiali.enabled` | Specifies whether Kiali addon should be installed | true/false | `false` |

## Uninstalling the Chart

To uninstall/delete the `istio` release:
```
$ helm delete istio
```
The command removes all the Kubernetes components associated with the chart and deletes the release.

To uninstall/delete the `istio` release completely and make its name free for later use:
```
$ helm delete istio --purge
```
