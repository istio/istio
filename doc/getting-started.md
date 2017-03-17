# Getting started with Istio

## Before you begin

This tutorial assumes you have a working Kubernetes of at least version 1.5.2.  Do `kubectl version` to verify
that you have a _kubectl_ command line and connectivity to a version 1.5.2 Kubernetes server.

Next, from the directory you cloned [https://github.com/istio/istio](https://github.com/istio/istio) into, 
bring up the Istio control plane

```bash
$ kubectl apply -f demos/istio
service "istio-ingress-controller" created
deployment "istio-ingress-controller" created
service "istio-manager" created
deployment "istio-manager" created
configmap "mixer-config" created
service "istio-mixer" created
deployment "istio-mixer" created
```

## Connecting microservices with Istio

This guide shows how to set up Istio and manipulate the proxy mesh to achieve useful behavior.
In this example we have two microservices

* microservice "hello" returns a content bit of JSON.
* microservice "frontend" calls microservice "hello"

The communicating microservices are examples from Kubernetes'
[Connecting a Front End to a Back End Using a Service](https://kubernetes.io/docs/tutorials/connecting-apps/connecting-frontend-backend/) tutorial.

First we will stand the microservices using the Istio pattern of a proxy within frontend's pod:

```
# Backend "hello" service
kubectl create -f http://k8s.io/docs/tutorials/connecting-apps/hello.yaml
kubectl create -f http://k8s.io/docs/tutorials/connecting-apps/hello-service.yaml

# Frontend service
kubectl create -f doc/frontend-and-proxy.yaml
kubectl expose -f doc/frontend-and-proxy.yaml --type=NodePort --name=frontend
```

<!---
Note that to stand up the frontend without Istio, we can do
kubectl run frontend --image=gcr.io/google-samples/hello-frontend:1.0 --port=8080 --overrides='{ "apiVersion": "extensions/v1beta1", "spec": { "containers": { "lifecycle": { "preStop": { "exec": { "command": ["/usr/sbin/nginx","-s","quit"] } } } } } }'
# or
kubectl run front-no-istio --image=gcr.io/google-samples/hello-frontend:1.0 --port=80
# and then
kubectl expose deployment front-no-istio --type=NodePort --name=frontend-no-istio
-->

At this point we have two microservices.  Let us test by looking up the public port

```
# Get the IP address.  (Alternate: gcloud compute instances list)
minikube ip
# Get the NodePorts for both original and Istio versions
kubectl describe services frontend-no-istio
kubectl describe services frontend
```

Test the service by doing `curl http://*ip*:*NodePort*`  For example, if the IP is 192.168.99.101 and NodePort is 31178/TCP, execute

```
$ curl 192.168.99.101:31778
{"message":"Hello"}
```

With Istio, the output is different **TODO it shouldn't be**

```
$ curl -i 192.168.99.101:31778
HTTP/1.1 404 Not Found
date: Fri, 17 Mar 2017 00:06:05 GMT
server: envoy
content-length: 0
```

The output JSON hello message came from the "hello" service via the "frontend" service.

## Manipulating the service mesh with route rules and destination polices

By default all traffic is sent, but let's TODO

```
# First, create a rule file
$ cat <<EOF > /tmp/hello-rule.yaml
destination: hellodemo.default.svc.cluster.local
route:
- tags:
    version: v1
  weight: 100
EOF

# Give the file to istioctl
istioctl create -f /tmp/rev-rule.yaml
```

## Discussion

TODO
