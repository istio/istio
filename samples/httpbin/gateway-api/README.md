# Configure helloworld using the Kubernetes Gateway API

Istio intends to make the Kubernetes [Gateway API](https://gateway-api.sigs.k8s.io/) the default API for traffic management [in the future](https://istio.io/latest/blog/2022/gateway-api-beta/).
You can use the following instructions to configure the ingress gateway and routing for the helloworld sample.

## Before you begin

The Gateway API CRDs do not come installed by default on most Kubernetes clusters, so install them if not present:

```bash
kubectl get crd gateways.gateway.networking.k8s.io || \
  { kubectl kustomize "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.5.0" | kubectl apply -f -; }
```

Also make sure you are running two versions (v1 and v2) of the helloworld service:

```bash
kubectl apply -f ../helloworld.yaml
```

## Configure the helloworld gateway

Apply the helloworld gateway configuration:

```bash
kubectl apply -f ./helloworld-gateway.yaml
```

Note that unlike an Istio `Gateway`, creating a Kubernetes `Gateway` resource will, by default, also [deploy an associated controller](https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api/#automated-deployment).

Set the INGRESS_HOST environment variables to the address of the helloworld gateway:

```bash
kubectl wait --for=condition=ready gtw helloworld-gateway
export INGRESS_HOST=$(kubectl get gtw helloworld-gateway -o jsonpath='{.status.addresses[*].value}')
```

Confirm the sample is running using curl:

```bash
for run in {1..10}; do curl http://$INGRESS_HOST/hello; done
```

Since no version routing has been configured, you should see an equal split of traffic, about half handled by helloworld-v1 and the other half handled by helloworld-v2.

## Configure weight-based routing

Declare the helloworld versions (Gateway API requires backend service definitions, unlike the Istio API which uses DestinationRule subsets for this):

```bash
kubectl apply -f ./helloworld-versions.yaml
```

Apply the following route rule to distribute the helloworld traffic 90% to v1, 10% to v2:

```bash
kubectl apply -f ./helloworld-route.yaml
```

Run the previous curl commands again:

```bash
for run in {1..10}; do curl http://$INGRESS_HOST/hello; done
```

Now you should see about 9 out of 10 requests handled by helloworld-v1 and only about 1 in 10 handled by helloworld-v2.

## Cleanup

```bash
kubectl delete -f ./helloworld-gateway.yaml
kubectl delete -f ./helloworld-versions.yaml
kubectl delete -f ../helloworld.yaml
```
