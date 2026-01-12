# Httpbin service

This sample runs [httpbin](https://httpbin.org) as an Istio service.
Httpbin is a well-known HTTP testing service that can be used for experimenting
with all kinds of Istio features.

This sample uses a fork of the [upstream httpbin repo](https://github.com/postmanlabs/httpbin) with [multiarch image support](https://github.com/mccutchen/go-httpbin).

To use it:

1. Install Istio by following the [istio install instructions](https://istio.io/docs/setup/).

1. Start the httpbin service inside the Istio service mesh:

    If you have [automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection) enabled:

    ```bash
    kubectl apply -f httpbin.yaml
    ```

    Otherwise, manually inject the sidecars before applying:

    ```bash
    kubectl apply -f <(istioctl kube-inject -f httpbin.yaml)
    ```

Because the httpbin service is not exposed outside the cluster
you cannot _curl_ it directly, however you can verify that it is working correctly using
a _curl_ command against `httpbin:8000` *from inside the cluster* using the public _dockerqa/curl_
image from Docker hub:

```bash
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/html
kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent --head httpbin:8000/status/500
time kubectl run -i --rm --restart=Never dummy --image=dockerqa/curl:ubuntu-trusty --command -- curl --silent httpbin:8000/delay/5
```

You can also test the httpbin service by starting the [sleep service](../sleep) and calling httpbin from it.

A third option is to access the service from the outside of the mesh through an Ingress Gateway.
The [Ingress Gateways](https://istio.io/docs/tasks/traffic-management/ingress/ingress-control/) task explains how to do it.
