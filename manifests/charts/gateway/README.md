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

To view support configuration options and documentation, run:

```console
helm show values istio/gateway
```

### Examples

#### Egress Gateway

Deploying a Gateway to be used as an [Egress Gateway](https://istio.io/latest/docs/tasks/traffic-management/egress/egress-gateway/):

```yaml
service:
  # Egress gateways do not need an external LoadBalancer IP
  type: ClusterIP
```

#### Multi-network/VM Gateway

Deploying a Gateway to be used as a [Multi-network Gateway](https://istio.io/latest/docs/setup/install/multicluster/) for network `network-1`:

```yaml
networkGateway: network-1
```

### Migrating from other installation methods