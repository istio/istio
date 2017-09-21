# Helloworld service

This sample runs two versions of a simple helloworld service that return their version and instance (hostname)
when called. It's used to demonstrate canary deployments working in conjuction with autoscaling.
See [Canary deployments using istio](https://istio.io/blog/canary-deployments-using-istio.html).

## Start the services
 
Note that kubernetes horizontal pod autosclalers only work if every container in the pods requests
cpu. Since the Istio proxy container added by kube-inject does not currently do it, we
need to edit the yaml before creating the deployment.

```bash
istioctl kube-inject -f helloworld.yaml -o helloworld-istio.yaml
```
Edit `helloworld-istio.yaml` to add the following to the proxy container
definition in both of the Deployment templates (helloworld-v1 and helloworld-v2)

```yaml
        resources:
          requests:
            cpu: 100m
```

Now create the deployment using the updated yaml file.

```bash
kubectl create -f helloworld-istio.yaml
```

Get the ingress URL and confirm it's running using curl.

```bash
export HELLOWORLD_URL=$(kubectl get po -l istio=ingress -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -o 'jsonpath={.spec.ports[0].nodePort}')
curl http://$HELLOWORLD_URL/hello
```

## Autoscale the services

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

## Cleanup

```bash
kubectl delete -f helloworld.yaml
kubectl delete hpa --all
```
