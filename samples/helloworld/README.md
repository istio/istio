# Helloworld service

This sample includes two versions of a simple helloworld service that returns its version
and instance (hostname) when called.
It can be used as a test service when experimenting with version routing.

This service is also used to demonstrate canary deployments working in conjunction with autoscaling.
See [Canary deployments using Istio](https://istio.io/blog/2017/0.1-canary).

## Start the helloworld service

The following commands assume you have
[automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection)
enabled in your cluster.
If not, you'll need to modify them to include
[manual sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#manual-sidecar-injection).

To run both versions of the helloworld service, use the following command:

```bash
kubectl apply -f helloworld.yaml
```

Alternatively, you can run just one version at a time by first defining the service:

```bash
kubectl apply -f helloworld.yaml -l service=helloworld
```

and then deploying version v1, v2, or both:

```bash
kubectl apply -f helloworld.yaml -l version=v1
kubectl apply -f helloworld.yaml -l version=v2
```

For even more flexibility, there is also a script, `gen-helloworld.sh`, that will
generate YAML for the helloworld service. This script takes the following
arguments:

| Argument              | Default | Description                                                            |
|-----------------------|---------|------------------------------------------------------------------------|
| `-h`,`--help`         |         | Prints usage information.                                              |
| `--version`           | `v1`    | Specifies the version that will be returned by the helloworld service. |
| `--includeService`    | `true`  | If `true` the service will be included in the YAML.                    |
| `--includeDeployment` | `true`  | If `true` the deployment will be included in the YAML.                 |

You can use this script to deploy a custom version:

```bash
./gen-helloworld.sh --version customversion | \
    kubectl apply -f -
```

## Configure the helloworld gateway

*___Note:___ Istio intends to make the Kubernetes [Gateway API](https://gateway-api.sigs.k8s.io/) the default API for traffic management [in the future](https://istio.io/latest/blog/2022/gateway-api-beta/). You can use the Gateway API to configure the helloworld service, instead of the classic Istio configuration model, by following the instructions in [./gateway-api/README.md](./gateway-api/README.md), instead of the instructions below.*

Apply the helloworld gateway configuration:

```bash
kubectl apply -f helloworld-gateway.yaml
```

Follow [these instructions](https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/#determining-the-ingress-ip-and-ports)
to set the INGRESS_HOST and INGRESS_PORT variables and then confirm the sample is running using curl:

```bash
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
curl http://$GATEWAY_URL/hello
```

## Autoscale the services

Note that a Kubernetes [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
only works if all containers in the pods request cpu. In this sample the deployment
containers in `helloworld.yaml` are configured with the request.
The injected istio-proxy containers also include cpu requests,
making the helloworld service ready for autoscaling.

Enable autoscaling on both versions of the service:

```bash
kubectl autoscale deployment helloworld-v1 --cpu-percent=50 --min=1 --max=10
kubectl autoscale deployment helloworld-v2 --cpu-percent=50 --min=1 --max=10
kubectl get hpa
```

## Generate load

```bash
./loadgen.sh &
./loadgen.sh & # run it twice to generate lots of load
```

Wait for about 2 minutes and then check the number of replicas:

```bash
kubectl get hpa
```

If the autoscaler is functioning correctly, the `REPLICAS` column should have a value > 1.

## Cleanup

```bash
kubectl delete -f helloworld.yaml
kubectl delete -f helloworld-gateway.yaml
kubectl delete hpa helloworld-v1 helloworld-v2
```
