# Httpbin service

This sample runs [httpbin](https://httpbin.org) as an Istio service. 
Httpbin is a well known HTTP testing service that can be used for experimenting
with all kinds of Istio features.

To use it:

1. Install Istio by following the [istio install instructions](https://istio.io/docs/setup/kubernetes/quick-start.html).

2. Start the httpbin service inside the Istio service mesh:

   ```bash
   kubectl apply -f <(istioctl kube-inject -f httpbin.yaml)
   ```
 
Because the httpbin service is not exposed outside of the cluster
we cannot _curl_ it directly, however we can verify that it is working correctly using
a _curl_ command against `httpbin:8000` *from inside the cluster* using the public _dockerqa/curl_
image from the Docker hub:

```bash
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/html
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/status/500
time kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/delay/5
```

Alternatively, you can test the httpbin service by
[configuring an ingress resource](https://istio.io/docs/tasks/traffic-management/ingress.html) or
by starting the [sleep service](../sleep) and calling httpbin from it.
