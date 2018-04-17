# Simple sleep service

This sample consists of a simple service that does nothing but sleep. 
It's a ubuntu container with curl installed that can be used as a request source for invoking other services
to experiment with Istio networking.
To use it:

1. Install Istio by following the [istio install instructions](https://istio.io/docs/setup/kubernetes/quick-start.html).

2. Start the sleep service:

   ```bash
   kubectl apply -f <(istioctl kube-inject -f sleep.yaml)
   ```
   
   Note that if you also want to be able to directly call
   external services, you'll need to set the `--includeIPRanges` option of `kube-inject`.
   See [configuring egress](https://istio.io/docs/tasks/traffic-management/egress.html) for details.
   
3. Start some other services, for example, the [Bookinfo sample](https://istio.io/docs/guides/bookinfo.html).

Now you can `kubectl exec` into the sleep service to experiment with Istio.
For example, the following commands can be used to call the Bookinfo `ratings` service:

```
export SLEEP_POD=$(kubectl get pod -l app=sleep -o jsonpath={.items..metadata.name})
kubectl exec -it $SLEEP_POD -c sleep curl http://ratings.default.svc.cluster.local:9080/ratings
{"Reviewer1":5,"Reviewer2":4}
```
