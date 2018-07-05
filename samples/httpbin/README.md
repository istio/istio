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

3. You can test the httpbin service by
   [creating an ingress resource](https://istio.io/docs/tasks/traffic-management/ingress.html):

   ```bash
   kubectl apply -f <(istioctl kube-inject -f httpbin-gateway.yaml)
   ```

4. Test the Ingress:

   ```bash
   $ curl -I http://${INGRESS_IP}/html
   HTTP/1.1 200 OK
   server: envoy
   date: Wed, 06 Jun 2018 18:34:43 GMT
   content-type: text/html; charset=utf-8
   content-length: 3741
   access-control-allow-origin: *
   access-control-allow-credentials: true
   x-envoy-upstream-service-time: 10
   ```

Alternatively, you can verify that it is working correctly using
a _curl_ command against `httpbin:8000` *from inside the cluster* using the public _dockerqa/curl_
image from the Docker hub:

```bash
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/html
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/status/500
time kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/delay/5
```
