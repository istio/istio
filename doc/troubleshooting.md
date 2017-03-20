# Troubleshooting services running under Istio

## This document is <span style="color:red">UNDER CONSTRUCTION</span>

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

## Interactive shell inside an Istio proxied container

Start a pod that includes an interactive environment and an Istio proxy.

```
# Interactive pod
kubectl create -f doc/frontend-debug-and-proxy.yaml
```

This starts up a Pod that includes an instance of <i>dockerqa/curl:ubuntu-trusty</i>, a Docker-provided
image ofr Ubuntu 14:04 with <i>curl</i> available from <bash>.  The instance just sleeps forever, but we can
bring up a shell inside the container and poke around.

```
# Find the pod's full name
kubectl get pods | grep frontend
# Bring up an interactive shell inside the pod
kubectl exec -it frontend-debug-with-istio-proxy-124106654-xr5ln /bin/bash
# (use control-d to log out of the pod)

# Bring up the proxy
kubectl exec --container proxy -it frontend-debug-with-istio-proxy-124106654-xr5ln /bin/bash
```



## Discussion

### The proxy container image used by the pod

The Docker image used by the Proxy SHOULD be identical to the image used by the manager.  If they are different,
sometimes communication from one app to another fails with timeout or 404.  To see the image used by the Manager do:

```
kubectl describe pod istio-manager- | grep Image:
```

(This assumes the Istio control plane is running.)

### Viewing the Envoy configuration for an Istio application

```
# First find the pod with `kubectl get pods`
POD=frontend-with-istio-proxy-1588773936-mdr2k
# Get the filename of the Envoy configuration
kubectl exec $POD --container proxy ls /etc/envoy
# View the Envoy configuration
kubectl exec $POD --container proxy cat /etc/envoy/envoy-rev1.json
```

