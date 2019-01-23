# Tornado - Demo Websockets App

This is a sample application that demonstrates the use of an upgraded websockets connection on an ingress traffic when using Istio `VirtualService`.
The `app.yaml` creates a Kubernetes `Service` and a `Deployment` that is based on an existing Docker image for [Hiroakis's Tornado Websocket Example](https://github.com/hiroakis/tornado-websocket-example).

__Notice:__ The addition of websockets upgrade support in v1alpha3 routing rules has only been added after the release of `Istio v0.8.0`.

## Prerequisites
- Install Istio by following the [Istio Quick Start](https://istio.io/docs/setup/kubernetes/quick-start.html).

## Installation
1. First install the application service:
   - With manual sidecar injection:
   ```command
   kubectl create -f <(istioctl kube-inject --debug -f samples/websockets/app.yaml)
   ```
   - With automatic sidecar injection:
   ```command
   kubectl create -f samples/websockets/app.yaml
   ```
2. Create the Ingress `Gateway` and `VirtualService` that enables the upgrade to Websocket for incoming traffic:
   ```command
   kubectl create -f samples/websockets/route.yaml
   ```

## Test
- [Find your ingress gateway IP](https://istio.io/docs/tasks/traffic-management/ingress/#determining-the-ingress-ip-and-ports)
- Access http://$GATEWAY_IP/ with your browser
- The `WebSocket status` should show a green `open` status which means  that a websocket connection to the server has been established.
To see the websocket in action see the instructions in the _REST API examples_ section of the demo app webpage for updating the server-side data and getting the updated data through the open websocket to the table in the webpage (without refreshing).

## Cleanup
```command
kubectl delete -f samples/websockets/route.yaml
```
```command
kubectl delete -f samples/websockets/app.yaml
```
