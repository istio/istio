# Istio Gateway Helm Chart

This chart installs an Istio gateway deployment.

## Setup Repo Info

```console
helm repo add istio https://istio-release.storage.googleapis.com/charts
helm repo update
```

_See [helm repo](https://helm.sh/docs/helm/helm_repo/) for command documentation._

## Installing the Chart

To install the chart with the release name `istio-ingressgateway`:

```console
helm install istio-ingressgateway istio/gateway
```

## Uninstalling the Chart

To uninstall/delete the `istio-ingressgateway` deployment:

```console
helm delete istio-ingressgateway
```

## Configuration

TODO 
| Parameter                                 | Description                                   | Default                                                 |
|-------------------------------------------|-----------------------------------------------|---------------------------------------------------------|
| `replicas`                                | Number of nodes                               | `1`                                                     |
| `podDisruptionBudget.minAvailable`        | Pod disruption minimum available              | `nil`                                                   |
| `podDisruptionBudget.maxUnavailable`      | Pod disruption maximum unavailable            | `nil`                                                   |
| `deploymentStrategy`                      | Deployment strategy                           | `{ "type": "RollingUpdate" }`                           |
| `livenessProbe`                           | Liveness Probe settings                       | `{ "httpGet": { "path": "/api/health", "port": 3000 } "initialDelaySeconds": 60, "timeoutSeconds": 30, "failureThreshold": 10 }` |
| `readinessProbe`                          | Readiness Probe settings                      | `{ "httpGet": { "path": "/api/health", "port": 3000 } }`|
| `securityContext`                         | Deployment securityContext                    | `{"runAsUser": 472, "runAsGroup": 472, "fsGroup": 472}`  |
| `priorityClassName`                       | Name of Priority Class to assign pods         | `nil`                                                   |
| `image.repository`                        | Image repository                              | `grafana/grafana`                                       |
| `image.tag`                               | Image tag (`Must be >= 5.0.0`)                | `8.0.3`                                                 |
| `image.sha`                               | Image sha (optional)                          | `80c6d6ac633ba5ab3f722976fb1d9a138f87ca6a9934fcd26a5fc28cbde7dbfa` |

### Examples

#### Egress Gateway

Deploying a Gateway to be used as an [Egress Gateway](https://istio.io/latest/docs/tasks/traffic-management/egress/egress-gateway/):

```yaml
service:
  # Egress gateways do not need an external LoadBalancer IP
  type: ClusterIP
```

#### Multi-network/VM Gateway

Deploying a Gateway to be used as a [Multi-network Gateway](https://istio.io/latest/docs/setup/install/multicluster/) for network `network-1`.

```yaml
labels:
  topology.istio.io/network: network-1
service:
  ports:
  - name: status-port
    port: 15021
    targetPort: 15021
  - name: tls
    port: 15443
    targetPort: 15443
  - name: tls-istiod
    port: 15012
    targetPort: 15012
  - name: tls-webhook
    port: 15017
    targetPort: 15017
```
