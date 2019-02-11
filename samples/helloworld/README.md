# Helloworld service

This sample runs two versions of a simple helloworld service that return their
version and instance (hostname) when called. It's used to demonstrate canary deployments
working in conjunction with autoscaling.
See [Canary deployments using Istio](https://istio.io/blog/2017/0.1-canary.html).

## Start the services

If you don't have [automatic sidecar injection](https://istio.io/docs/setup/kubernetes/sidecar-injection.html#automatic-sidecar-injection)
set in your cluster you will need to manually inject it to the services:

```bash
istioctl kube-inject -f helloworld.yaml -o helloworld-istio.yaml
```

Note that Kubernetes [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
only work if all containers in the pods requests cpu. In this sample the deployment
containers within the `helloworld.yaml` are pre-defined with the request. The (manually/automatically)
injected istio-proxy containers also have the requests cpu therefore making the `helloworld`
ready for autoscaling.

Now create the deployment using the updated yaml file and create the gateway configuration:

```bash
kubectl create -f helloworld-istio.yaml
kubectl create -f helloworld-gateway.yaml
```

Follow the [instructions](https://preliminary.istio.io/docs/tasks/traffic-management/ingress.html#determining-the-ingress-ip-and-ports) to set the INGRESS_HOST and INGRESS_PORT variables then confirm it's running using curl.

```bash
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
curl http://$GATEWAY_URL/hello
```

## Autoscale the services

Enable autoscale on both services:

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

Wait for about 2min and check the number of replicas:

```bash
kubectl get hpa
```

If autoscaler is functioning correctly the `REPLICAS` column should have a
value > 1.

## Cleanup

```bash
kubectl delete -f helloworld-istio.yaml
kubectl delete -f helloworld-gateway.yaml
kubectl delete hpa helloworld-v1 helloworld-v2
```
