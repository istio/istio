# Getting started with Istio

## This document is <span style="color:red">UNDER CONSTRUCTION</span>

## Before you begin

This tutorial assumes you have a working Kubernetes of at least version 1.5.2.  Do `kubectl version` to verify
that you have a _kubectl_ command line and connectivity to a version 1.5.2 Kubernetes server.

Bring up the *Istio control plane* from the directory you cloned [https://github.com/istio/istio](https://github.com/istio/istio) into:

```bash
$ kubectl apply -f kubernetes/istio-install
service "istio-manager" created
deployment "istio-manager" created
configmap "mixer-config" created
service "istio-mixer" created
deployment "istio-mixer" created
```

## Connecting microservices with Istio

This guide shows how to set up Istio and manipulate the proxy mesh to achieve useful behavior.
In this example we use two microservices:

* microservice "hello" returns a constant bit of JSON.
* microservice "frontend" calls microservice "hello"

The communicating microservices are examples from Kubernetes'
[Connecting a Front End to a Back End Using a Service](https://kubernetes.io/docs/tutorials/connecting-apps/connecting-frontend-backend/) tutorial.

First we will start the microservices using the Istio pattern of a
proxy within frontend's pod. The `istioctl kube-inject` command
injects the istio runtime proxy into Kubernetes resource files. It is
documented [here](https://istio.io/docs/reference/istioctl.html#kube-inject).

```bash

# Backend "hello" service with an instance of container gcr.io/istio-testing/runtime:demo
kubectl create -f <(istioctl kube-inject -f doc/hello-and-proxy.yaml)

# "Frontend" service with an instance of container gcr.io/istio-testing/runtime:demo
kubectl create -f <(istioctl kube-inject -f doc/frontend-and-proxy.yaml)
kubectl expose -f <(istioctl kube-inject -f doc/frontend-and-proxy.yaml) --type=NodePort --name=frontend

```

<!---
Note that to stand up the frontend without Istio, we can do
kubectl run frontend --image=gcr.io/google-samples/hello-frontend:1.0 --port=8080 --overrides='{ "apiVersion": "extensions/v1beta1", "spec": { "containers": { "lifecycle": { "preStop": { "exec": { "command": ["/usr/sbin/nginx","-s","quit"] } } } } } }'
# or
kubectl run front-no-istio --image=gcr.io/google-samples/hello-frontend:1.0 --port=80
# and then
kubectl expose deployment front-no-istio --type=NodePort --name=frontend-no-istio
kubectl describe services frontend-no-istio
-->

At this point we have two microservices.  Let us test by looking up the public port

```bash
# Get the IP address.  (Alternate: gcloud compute instances list)
minikube ip
export IP=`minikube ip`
# Get the NodePorts for both original and Istio versions
kubectl describe services frontend
export NODEPORT=...
```

Test the service by doing `curl -i http://$IP:$NODEPORT`  For example, if the IP is 192.168.99.101 and NodePort is 31178/TCP, execute

```bash
$ curl 192.168.99.101:31778
{"message":"Hello"}
```

The output JSON hello message came from the "hello" service via the "frontend" service.

## Manipulating the service mesh with route rules and destination polices

Istio has many advanced capabilities to control how network traffic flows between microservices.  One of the
most simple capabilities is to add a delay.

```
# First, create a rule file
cat <<EOF > /tmp/hello-5s-rule.yaml
type: route-rule
name: hello-5s-rule
spec:
  destination: hello.default.svc.cluster.local
  http_fault:
    delay:
      percent: 100
      fixed_delay_seconds: 5
EOF

# Give the file to istioctl
istioctl create -f /tmp/hello-5s-rule.yaml
```

Wait a few seconds, then issue `curl $IP:$NODEPORT` again.  This time it takes five seconds for *frontend* to reach *hello*.


## Discussion

TODO
