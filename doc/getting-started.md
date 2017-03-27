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
In this example we use *CouchDB* and *Nginx* as and demonstrate how to manipulate them:

* CouchDB is the official CouchDB Docker image
* Nginx is the offical Nginx Docker image.  We will configure it to redirect all calls to CouchDB

We will test as we go along.  To test we will need the IP address of a Kubernetes node.  Before we begin, get that address.

```
# Get the IP address.
minikube ip
# Alternate for IBM Container Service: `bx cs workers <MYCLUSTER>` and use the public address
# Alternet for Google compute cloud `gcloud compute instances list`)
export IP=`minikube ip`
```

First, let's start CouchDB and have it join the Istio service mesh

```
# Start the official Docker CouchDB image the container in a Kubernetes Pod
kubectl run couchdb --image=docker.io/library/couchdb
```

The _kubectl get deployment_ subcommand simply creates a machine readable description of a service.  The _istioctl kube-inject_ subcommand creates a machine readable description of a service including Istio components.  By piping the commands together and sending the output to _kubectl apply_ we add Istio capabilities to our service.

```
# The CouchDB deployment must join the Istio service mesh.  Note that the pod will be recreated.
kubectl get deployment couchdb --output yaml | istioctl kube-inject --filename - --tag 2017-03-22-17.30.06 | kubectl apply -f -
```

For this tutorial an Nginx front-end will talk to CouchDB.  To enable talking we must expose the deployment
as a Kubernetes Service.  For this demo we expose as a _NodePort_ so that we may show the behavior independently,
but this is not needed.

```
# Expose port 5984 (the CouchDB port) on the pod to a random port on the Kubernetes node
kubectl expose deployment couchdb --port=5984 --name=couchdb --type=NodePort
# Get the port on that node that exposes port 5984 on the CouchDB container
COUCHDB_NODEPORT=$(kubectl get service couchdb --output jsonpath='{.spec.ports[0].nodePort}')
curl $IP:$COUCHDB_NODEPORT
```

Next we will start Nginx and configure it to proxy for the CouchDB service started in the previous step

```
# Create an nginx service
kubectl run nginx --image=docker.io/nginx
# The Nginx deployment must join the Istio service mesh.  Note that the pod will be recreated.
kubectl get deployment nginx --output yaml | istioctl kube-inject --filename - --tag 2017-03-22-17.30.06 | kubectl apply -f -
# WAIT A FEW SECONDS
# Configure the nginx to point to the CouchDB.  Note that this step is just for the demo.  You should
# not make changes to running containers in production, because those changes are lost if the pod restarts.
NGINX_POD=$(kubectl get pods -l run=nginx --output jsonpath='{.items[*].metadata.name}')
kubectl exec -it $NGINX_POD /bin/bash -- -c \
   "echo 'server { listen 9080; location / { proxy_pass http://couchdb:5984; proxy_http_version 1.1;  } }' > /etc/nginx/conf.d/frontend.conf && /usr/sbin/nginx -s reload"
# (Note: The previous line should response with 'signal process started')
# Expose the Nginx
kubectl expose deployment nginx --type=NodePort --port 9080
NGINX_NODEPORT=$(kubectl get service nginx --output jsonpath='{.spec.ports[0].nodePort}')
curl $IP:$NGINX_NODEPORT
```

At this point we have two services.  Both are wired through the Istio service mesh.  Both output the same data -- the CouchDB home JSON response.

## Manipulating the service mesh with route rules and destination polices

Istio has many advanced capabilities to control how network traffic flows between microservices.  One of the
most simple capabilities is to add a delay.

```
# First, create a rule file
cat <<EOF > /tmp/couch-5s-rule.yaml
type: route-rule
name: couch-5s-rule
spec:
  destination: couchdb.default.svc.cluster.local
  http_fault:
    delay:
      percent: 100
      fixed_delay_seconds: 5
EOF

# Give the file to istioctl
istioctl create -f /tmp/couch-5s-rule.yaml
```

Wait a few seconds, then issue `curl $IP:$NGINX_NODEPORT` again.  This time it takes five seconds for *frontend* to reach *hello*.

The rule file defines a 5 second delay to be added to 100% of network traffic with the destination host name 
"couchdb.default.svc.cluster.local".  Although the Nginx configuration only uses "couchdb", the Istio configuration
requires the full format "<host>.<namespace>.svc.cluster.local" when running on Kubernetes.  The _kube-dns_ service on
Kubernetes sets up "<namespace>.svc.cluster.local" as the DNS search path so the names are equivelent.

## Cleanup

To remove the deployments and services used in this tutorial:

```
kubectl delete service couchdb
kubectl delete deployment couchdb
kubectl delete service nginx
kubectl delete deployment nginx
kubectl delete -f kubernetes/istio-install # OPTIONAL
```

## Discussion

TODO
